package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// PricingSettingsRepo 提供定价读写以及原子审计事务。
type PricingSettingsRepo interface {
	// GetAll 返回全部设置。
	GetAll(ctx context.Context) (map[string]string, error)
	// WithPricingTx 串行执行定价配置变更，不在事务中调用外部网络。
	WithPricingTx(ctx context.Context, fn func(repository.PricingTx) error) error
}

// PricingSettingsService 管理人工定价规则，不改写系统拉取的汇率。
type PricingSettingsService struct {
	repo PricingSettingsRepo
	now  func() time.Time
}

// NewPricingSettingsService 创建定价设置服务；时钟可注入以验证过期规则。
func NewPricingSettingsService(repo PricingSettingsRepo, now func() time.Time) *PricingSettingsService {
	if now == nil {
		now = time.Now
	}
	return &PricingSettingsService{repo: repo, now: now}
}

// Get 返回定价规则与汇率快照。
func (s *PricingSettingsService) Get(ctx context.Context) (*model.PricingSettingsView, error) {
	// 1. 同一次读取得到规则和最近成功汇率，避免混用不同时间的值。
	kv, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return pricingView(kv, s.now()), nil
}

// ValidatePricingSettings 校验契约范围与小数位，NaN 和无限大也不接受。
func ValidatePricingSettings(req model.PricingSettingsReq) error {
	if !positivePrecision(req.CreditsPerYuan, 100) {
		return errcode.ErrInvalidParams.WithMsg("credits_per_yuan 必须大于 0 且最多两位小数")
	}
	if req.DefaultMarkupPct < 0 || req.DefaultMarkupPct > 1000 {
		return errcode.ErrInvalidParams.WithMsg("default_markup_pct 必须为 0–1000 的整数")
	}
	if req.USDCNYOverride != nil && !positivePrecision(*req.USDCNYOverride, 10000) {
		return errcode.ErrInvalidParams.WithMsg("usd_cny_override 必须大于 0 且最多四位小数")
	}
	return nil
}

// positivePrecision 使用容差消除二进制表示误差，但不接受额外小数位。
func positivePrecision(n, scale float64) bool {
	return n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) && math.Abs(n*scale-math.Round(n*scale)) < 0.0000001
}

// PricingSettingsBody 返回只有人工字段的稳定快照；后台汇率刷新不应导致人工配置 SHA 冲突。
func PricingSettingsBody(v *model.PricingSettingsView) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"credits_per_yuan": v.CreditsPerYuan, "default_markup_pct": v.DefaultMarkupPct, "usd_cny_override": v.USDCNYOverride})
	return b
}

// Update 原子更新定价设置并写审计；base 非空时执行审批的二次冲突检测。
// detail 由审批服务提供关联信息；普通后台 PUT 传 nil。
func (s *PricingSettingsService) Update(ctx context.Context, actorID uint64, req model.PricingSettingsReq, base string, detail map[string]any) (*model.PricingSettingsView, uint64, error) {
	// 1. 直接接口和助手共用同一校验，避免模型绕过设置范围。
	if err := ValidatePricingSettings(req); err != nil {
		return nil, 0, err
	}
	return s.update(ctx, actorID, req, base, detail, false)
}

// Restore 仅供已验签的审计反向单恢复快照，允许将积分比例恢复为“尚未设置”。
func (s *PricingSettingsService) Restore(ctx context.Context, actorID uint64, raw json.RawMessage, base string, detail map[string]any) (*model.PricingSettingsView, uint64, error) {
	// 1. null 只代表恢复历史的未设置状态，其他字段仍经过普通接口同一校验。
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return nil, 0, errcode.ErrInvalidParams
	}
	unset := fields["credits_per_yuan"] == nil
	if unset {
		fields["credits_per_yuan"] = 1
	}
	checked, _ := json.Marshal(fields)
	var req model.PricingSettingsReq
	if json.Unmarshal(checked, &req) != nil {
		return nil, 0, errcode.ErrInvalidParams
	}
	if err := ValidatePricingSettings(req); err != nil {
		return nil, 0, err
	}
	// 2. 原子的摘要检测与审计流程仍然完全相同，不绕过并发控制。
	return s.update(ctx, actorID, req, base, detail, unset)
}

// update 以同一事务保存普通设置或已审批的历史快照。
func (s *PricingSettingsService) update(ctx context.Context, actorID uint64, req model.PricingSettingsReq, base string, detail map[string]any, unset bool) (*model.PricingSettingsView, uint64, error) {
	var view *model.PricingSettingsView
	var auditID uint64
	// 2. 加锁后先查幂等审计，再比摘要；变更和审计在同一个事务内。
	err := s.repo.WithPricingTx(ctx, func(tx repository.PricingTx) error {
		var err error
		auditID, err = pricingReplay(ctx, tx, detail)
		if err != nil || auditID != 0 {
			return err
		}
		kv, err := tx.GetAll(ctx)
		if err != nil {
			return err
		}
		before := PricingSettingsBody(pricingView(kv, s.now()))
		if base != "" && base != configbody.SHA256(before) {
			return repository.ErrRevisionConflict
		}
		values := pricingValues(req, unset)
		if err := tx.SetMany(ctx, values, actorID); err != nil {
			return err
		}
		for k, v := range values {
			kv[k] = v
		}
		view = pricingView(kv, s.now())
		data := map[string]any{"before": json.RawMessage(before), "after": PricingSettingsBody(view)}
		for k, v := range detail {
			data[k] = v
		}
		raw, _ := json.Marshal(data)
		row := &model.AdminAuditLog{ActorID: actorID, Action: model.AdminAuditSettingsPricing, TargetType: model.AdminAuditTargetSettings, DetailJSON: model.JSONText(raw)}
		if err := tx.Insert(ctx, row); err != nil {
			return err
		}
		auditID = row.ID
		return nil
	})
	// 3. 重放只需返回当前视图，审计 ID 沿用首次执行值。
	if err == nil && view == nil {
		view, err = s.Get(ctx)
	}
	return view, auditID, err
}

// pricingReplay 查找审批条目的首次审计，没有历史时继续执行而不是报错。
func pricingReplay(ctx context.Context, tx repository.PricingTx, detail map[string]any) (uint64, error) {
	if detail == nil {
		return 0, nil
	}
	aid, _ := detail["approval_id"].(string)
	index, _ := detail["index"].(int)
	row, err := tx.FindApplied(ctx, aid, index)
	if errors.Is(err, repository.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return row.ID, nil
}

// pricingValues 只更新人工维护的三个键，绝不覆盖系统拉取的汇率。
func pricingValues(req model.PricingSettingsReq, unset bool) map[string]string {
	values := map[string]string{model.SettingPricingCreditsPerYuan: strconv.FormatFloat(req.CreditsPerYuan, 'f', -1, 64), model.SettingPricingMarkup: strconv.Itoa(req.DefaultMarkupPct), model.SettingPricingFXOverride: ""}
	if unset {
		values[model.SettingPricingCreditsPerYuan] = ""
	}
	if req.USDCNYOverride != nil {
		values[model.SettingPricingFXOverride] = strconv.FormatFloat(*req.USDCNYOverride, 'f', -1, 64)
	}
	return values
}

// pricingView 对缺省配置给出契约默认值，只有成功拉到的汇率才标为 live。
func pricingView(kv map[string]string, now time.Time) *model.PricingSettingsView {
	v := &model.PricingSettingsView{DefaultMarkupPct: 30, USDCNY: model.PricingFXView{Source: "none"}}
	v.CreditsPerYuan = settingPositive(kv[model.SettingPricingCreditsPerYuan])
	v.USDCNYOverride = settingPositive(kv[model.SettingPricingFXOverride])
	if n, err := strconv.Atoi(kv[model.SettingPricingMarkup]); err == nil && n >= 0 && n <= 1000 {
		v.DefaultMarkupPct = n
	}
	if at, err := time.Parse(time.RFC3339, kv[model.SettingPricingFXFetchedAt]); err == nil {
		text := at.UTC().Format(time.RFC3339)
		v.USDCNY.FetchedAt = &text
		v.USDCNY.Stale = now.Sub(at) > 7*24*time.Hour
	}
	if v.USDCNY.Rate = settingPositive(kv[model.SettingPricingFXLast]); v.USDCNY.Rate != nil {
		v.USDCNY.Source = "live"
		if date := kv[model.SettingPricingFXDate]; date != "" {
			v.USDCNY.Date = &date
		}
	}
	if v.USDCNYOverride != nil {
		v.USDCNY.Rate = v.USDCNYOverride
		v.USDCNY.Source = "override"
		v.USDCNY.Date = nil
		v.USDCNY.Stale = false
	}
	return v
}

// settingPositive 忽略损坏的非正数值，按尚未设置处理。
func settingPositive(raw string) *float64 {
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
