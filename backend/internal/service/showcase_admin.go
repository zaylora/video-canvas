package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// 展示条目字段的长度与取值上限（单位：字符数 / 秒）。
const (
	maxShowcasePromptRunes = 80   // 文案最多 80 个字符：它直接叠在登录页画面上，再长就排不下
	maxShowcaseLabelRunes  = 40   // 模型标注最多 40 个字符
	maxShowcaseStartSec    = 3600 // 起始秒上限，一小时足够覆盖任何登录页片段，同时挡住明显的误填
)

// CreateItem 新增一个展示条目并返回它的后台视图。
// 视频必须是 video 素材，且是当前管理员自己的，或平台生成的（素材库里挑选，任何用户的都行）；
// 封面（可选）必须是当前管理员自己的 image 素材；新条目排在最后。
func (s *ShowcaseService) CreateItem(ctx context.Context, actorID uint64, req *model.CreateShowcaseItemReq) (*model.ShowcaseAdminItem, error) {
	// 1. 校验并规整文案、模型标注、起始秒（文案和标注先去首尾空白再按字符数计）
	prompt, err := normalizeShowcasePrompt(req.Prompt)
	if err != nil {
		return nil, err
	}
	label, err := normalizeShowcaseLabel(req.ModelLabel)
	if err != nil {
		return nil, err
	}
	if err := checkShowcaseStartSec(req.StartSec); err != nil {
		return nil, err
	}

	// 2. 校验素材归属与类型：不可见的素材（别人上传的）和不存在的素材统一按“素材不存在”处理（A7）
	video, err := s.showcaseVideo(ctx, actorID, req.AssetID)
	if err != nil {
		return nil, err
	}
	assets := map[uint64]*model.Asset{video.ID: video}
	if req.PosterAssetID != nil {
		poster, err := s.ownedAsset(ctx, actorID, *req.PosterAssetID, "image", "封面素材")
		if err != nil {
			return nil, err
		}
		assets[poster.ID] = poster
	}

	// 3. 新条目排在最后：sort = 当前最大 sort + 1（没有条目时 MaxSort 返回 -1，所以第一个条目是 0）。
	//    只有管理员会写，并发新增时 sort 偶尔相同也无妨：读取时 sort 相同按 id 升序，顺序仍然确定
	top, err := s.items.MaxSort(ctx)
	if err != nil {
		return nil, err
	}

	// 4. 入库；enabled 缺省为 true（新加的条目通常希望立即生效）
	it := &model.ShowcaseItem{
		AssetID: req.AssetID, PosterAssetID: req.PosterAssetID, Prompt: prompt, ModelLabel: label,
		StartSec: req.StartSec, Enabled: req.Enabled == nil || *req.Enabled, Sort: top + 1, CreatedBy: actorID,
	}
	if err := s.items.Create(ctx, it); err != nil {
		return nil, err
	}

	// 5. 审计：只记条目 id、素材 id 和文案（没有敏感信息）
	adminAudit(ctx, s.audit, actorID, model.AdminAuditShowcaseCreate, model.AdminAuditTargetShowcase, it.ID, map[string]any{
		"asset_id": it.AssetID, "prompt": it.Prompt, "enabled": it.Enabled,
	})
	return s.adminItem(ctx, it, assets)
}

// UpdateItem 部分更新一个展示条目并返回最新的后台视图：只改请求里出现的字段。
// 封面用 OptionalID 区分三种情况：没传 = 不改，null = 清空，数字 = 换成该图片素材。
// asset_id 用来替换视频（没传 = 不改），规则同新增；替换后排序、启用状态保持不变，也不会自动改封面（由调用方同时传封面）。
func (s *ShowcaseService) UpdateItem(ctx context.Context, actorID, id uint64, req *model.UpdateShowcaseItemReq) (*model.ShowcaseAdminItem, error) {
	// 1. 校验并收集要更新的列；一个字段都没传没有意义，按参数错误返回，避免产生一条“什么都没改”的审计
	fields, err := showcaseUpdateFields(req)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 && !req.PosterAssetID.Set && req.AssetID == nil {
		return nil, errcode.ErrShowcaseInvalid.WithMsg("请至少修改一个字段")
	}

	// 2. 条目必须存在
	if _, err := s.getItem(ctx, id); err != nil {
		return nil, err
	}

	// 3. 封面：null 清空（列置 NULL）；数字则必须是当前管理员的 image 素材
	if req.PosterAssetID.Set {
		if req.PosterAssetID.ID == nil {
			fields["poster_asset_id"] = nil
		} else {
			if _, err := s.ownedAsset(ctx, actorID, *req.PosterAssetID.ID, "image", "封面素材"); err != nil {
				return nil, err
			}
			fields["poster_asset_id"] = *req.PosterAssetID.ID
		}
	}

	// 4. 替换视频：规则同新增（自己的视频或平台生成的视频）。校验放在写库之前，任何一项不通过都不会产生部分更新
	if req.AssetID != nil {
		video, err := s.showcaseVideo(ctx, actorID, *req.AssetID)
		if err != nil {
			return nil, err
		}
		fields["asset_id"] = video.ID
	}

	// 5. 更新；第 2 步到这里之间条目被别人删掉时同样返回 404
	if err := s.items.Update(ctx, id, fields); err != nil {
		return nil, translateShowcaseNotFound(err)
	}

	// 6. 审计：记下被修改的列和新值
	adminAudit(ctx, s.audit, actorID, model.AdminAuditShowcaseUpdate, model.AdminAuditTargetShowcase, id, map[string]any{"fields": fields})

	// 7. 重新读取并返回最新视图
	it, err := s.getItem(ctx, id)
	if err != nil {
		return nil, err
	}
	assets, err := s.assetMap(ctx, []model.ShowcaseItem{*it})
	if err != nil {
		return nil, err
	}
	return s.adminItem(ctx, it, assets)
}

// DeleteItem 删除一个展示条目，只删条目本身，不删它引用的素材（素材可能还在别处使用）。
func (s *ShowcaseService) DeleteItem(ctx context.Context, actorID, id uint64) error {
	// 1. 删除；条目不存在返回 404
	if err := s.items.Delete(ctx, id); err != nil {
		return translateShowcaseNotFound(err)
	}

	// 2. 审计
	adminAudit(ctx, s.audit, actorID, model.AdminAuditShowcaseDelete, model.AdminAuditTargetShowcase, id, nil)
	return nil
}

// Reorder 按 ids 的顺序重写全部条目的 sort（0..n-1）。
// ids 必须恰好包含现有全部条目各一次：多了、少了、重复都按 54003 拒绝，防止前端拿着过期列表把新增的条目排没了。
func (s *ShowcaseService) Reorder(ctx context.Context, actorID uint64, ids []uint64) error {
	// 1. 在一个事务里“加锁读取现有 id → 校验 → 逐条重写 sort”：校验与写入必须在同一把锁内，
	//    否则校验通过后别的请求新增 / 删除条目，会得到不完整的排序。校验失败时事务回滚，没有任何写入
	err := s.items.WithTx(ctx, func(tx repository.ShowcaseTx) error {
		current, err := tx.ListIDs(ctx)
		if err != nil {
			return err
		}
		if !sameShowcaseIDs(current, ids) {
			return errcode.ErrShowcaseOrderMismatch
		}
		for i, id := range ids {
			if err := tx.SetSort(ctx, id, i); err != nil {
				return fmt.Errorf("更新第 %d 个条目的排序失败：%w", i+1, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 2. 审计：记下新的顺序
	adminAudit(ctx, s.audit, actorID, model.AdminAuditShowcaseOrder, model.AdminAuditTargetShowcase, 0, map[string]any{"ids": ids})
	return nil
}

// UpdateSettings 保存展示设置（仅 super_admin 可调，由路由保证），写审计并返回最新设置。
func (s *ShowcaseService) UpdateSettings(ctx context.Context, actorID uint64, req *model.UpdateShowcaseSettingsReq) (*model.ShowcaseSettingsView, error) {
	// 1. 三个字段都必填（handler 的 binding 已拦一道，这里兜底，避免漏传被当成 false 悄悄关掉开关）；秒数必须在 4–15
	if req.ClipSeconds == nil || req.ShowOnLogin == nil || req.PosterOnlyOnSaveData == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("片段秒数、登录页展示开关、省流量只显示封面都必填")
	}
	if !validShowcaseClipSeconds(*req.ClipSeconds) {
		return nil, errcode.ErrShowcaseInvalid.WithMsg(fmt.Sprintf("片段秒数必须在 %d 到 %d 之间", minShowcaseClipSeconds, maxShowcaseClipSeconds))
	}

	// 2. 记下改前的值用于审计，再用 SetMany 一次性写入（一个事务，三个键要么都生效要么都不生效）
	before, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	err = s.settings.SetMany(ctx, map[string]string{
		model.SettingShowcaseClipSeconds:      strconv.Itoa(*req.ClipSeconds),
		model.SettingShowcaseShowOnLogin:      strconv.FormatBool(*req.ShowOnLogin),
		model.SettingShowcasePosterOnSaveData: strconv.FormatBool(*req.PosterOnlyOnSaveData),
	}, actorID)
	if err != nil {
		return nil, err
	}

	// 3. 审计：只记前后值（都是开关与数字，没有敏感信息）
	after := &model.ShowcaseSettingsView{ClipSeconds: *req.ClipSeconds, ShowOnLogin: *req.ShowOnLogin, PosterOnlyOnSaveData: *req.PosterOnlyOnSaveData}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditShowcaseSettings, model.AdminAuditTargetSettings, 0, map[string]any{
		"before": before, "after": after,
	})
	return after, nil
}

// getItem 按 id 取条目，不存在翻译成 54001。
func (s *ShowcaseService) getItem(ctx context.Context, id uint64) (*model.ShowcaseItem, error) {
	it, err := s.items.GetByID(ctx, id)
	if err != nil {
		return nil, translateShowcaseNotFound(err)
	}
	return it, nil
}

// translateShowcaseNotFound 把 repository 的“不存在”翻译成 54001（404），其他错误原样返回。
func translateShowcaseNotFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrShowcaseNotFound
	}
	return err
}

// showcaseVideo 取可以作为展示视频的素材：必须是 video，并且是当前管理员自己的（任何来源），或平台生成的（source=generated，任何用户的）。
// 之所以放开“平台生成”：后台素材库允许管理员直接挑选全平台生成的视频做展示作品，这些视频不在管理员名下。
// 其余（别人上传的、不存在的）统一返回“素材不存在”，不暴露别人的素材是否存在（A7）；对管理员可见的素材类型不符返回 54002。
func (s *ShowcaseService) showcaseVideo(ctx context.Context, actorID, id uint64) (*model.Asset, error) {
	rows, err := s.assets.ListByIDs(ctx, []uint64{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || (rows[0].UserID != actorID && rows[0].Source != model.AssetSourceGenerated) {
		return nil, errcode.ErrAssetNotFound
	}
	if rows[0].Kind != model.KindVideo {
		return nil, errcode.ErrShowcaseInvalid.WithMsg("视频素材必须是视频")
	}
	return &rows[0], nil
}

// ownedAsset 取当前管理员名下指定类型（video / image）的素材。
// 素材不存在和不属于当前管理员统一返回“素材不存在”，不暴露别人的素材是否存在（A7）；类型不符返回 54002。
func (s *ShowcaseService) ownedAsset(ctx context.Context, actorID, id uint64, kind, what string) (*model.Asset, error) {
	a, err := s.assets.GetByID(ctx, actorID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrAssetNotFound
		}
		return nil, err
	}
	if a.Kind != kind {
		want := map[string]string{"video": "视频", "image": "图片"}[kind]
		return nil, errcode.ErrShowcaseInvalid.WithMsg(what + "必须是" + want)
	}
	return a, nil
}

// showcaseUpdateFields 校验更新请求里出现的文本类字段，返回要更新的列（列名 -> 新值）；封面由调用方单独处理。
func showcaseUpdateFields(req *model.UpdateShowcaseItemReq) (map[string]any, error) {
	fields := map[string]any{}
	if req.Prompt != nil {
		prompt, err := normalizeShowcasePrompt(*req.Prompt)
		if err != nil {
			return nil, err
		}
		fields["prompt"] = prompt
	}
	if req.ModelLabel != nil {
		label, err := normalizeShowcaseLabel(*req.ModelLabel)
		if err != nil {
			return nil, err
		}
		fields["model_label"] = label
	}
	if req.StartSec != nil {
		if err := checkShowcaseStartSec(*req.StartSec); err != nil {
			return nil, err
		}
		fields["start_sec"] = *req.StartSec
	}
	if req.Enabled != nil {
		fields["enabled"] = *req.Enabled
	}
	return fields, nil
}

// normalizeShowcasePrompt 去首尾空白后校验文案长度（按字符数，1–80）。
func normalizeShowcasePrompt(s string) (string, error) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < 1 || n > maxShowcasePromptRunes {
		return "", errcode.ErrShowcaseInvalid.WithMsg(fmt.Sprintf("文案需要 1 到 %d 个字", maxShowcasePromptRunes))
	}
	return s, nil
}

// normalizeShowcaseLabel 去首尾空白后校验模型标注长度（按字符数，0–40，可空）。
func normalizeShowcaseLabel(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxShowcaseLabelRunes {
		return "", errcode.ErrShowcaseInvalid.WithMsg(fmt.Sprintf("模型标注最多 %d 个字", maxShowcaseLabelRunes))
	}
	return s, nil
}

// checkShowcaseStartSec 校验起始秒在 0–3600 之间。
func checkShowcaseStartSec(sec float64) error {
	if sec < 0 || sec > maxShowcaseStartSec {
		return errcode.ErrShowcaseInvalid.WithMsg(fmt.Sprintf("起始秒必须在 0 到 %d 之间", maxShowcaseStartSec))
	}
	return nil
}

// sameShowcaseIDs 判断 ids 是否恰好是 current 的一个排列：数量相同、每个都存在、没有重复。
func sameShowcaseIDs(current, ids []uint64) bool {
	if len(current) != len(ids) {
		return false
	}
	pending := make(map[uint64]struct{}, len(current))
	for _, id := range current {
		pending[id] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := pending[id]; !ok {
			return false // 不存在的 id，或同一个 id 出现第二次（第一次已经从 pending 里删掉）
		}
		delete(pending, id)
	}
	return true
}
