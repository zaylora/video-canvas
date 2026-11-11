package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// 公告字段的长度上限与公开展示条数（设计：docs/design/公告/公告.md §8.3）。
const (
	maxAnnouncementTitleRunes = 60 // 标题最多 60 个字符：通栏只有一行，再长窄屏上要滚很久
	maxPublicAnnouncements    = 5  // 用户端最多轮播 5 条：再多一圈要半分钟以上，后面的几乎没人看到
)

// AnnouncementRepo 是公告的数据访问接口（真实实现是 repository.AnnouncementRepository）。
type AnnouncementRepo interface {
	// ListAll 返回全部未删除公告（含停用），按 sort、id 升序。
	ListAll(ctx context.Context) ([]model.Announcement, error)
	// GetByID 按 id 查询，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.Announcement, error)
	// MaxSort 返回当前最大的 sort，没有公告时返回 -1。
	MaxSort(ctx context.Context) (int, error)
	// Create 插入公告，成功后 ID、CreatedAt、UpdatedAt 已回填。
	Create(ctx context.Context, a *model.Announcement) error
	// Update 按列名更新指定字段（值为 nil 置 NULL）并刷新 updated_at，公告不存在返回 repository.ErrNotFound。
	Update(ctx context.Context, id uint64, fields map[string]any) error
	// SetEnabled 只改启用开关与修改人，不刷新 updated_at；公告不存在返回 repository.ErrNotFound。
	SetEnabled(ctx context.Context, id uint64, enabled bool, actorID uint64) error
	// Delete 软删除公告，不存在返回 repository.ErrNotFound。
	Delete(ctx context.Context, id uint64) error
	// WithTx 在一个事务里执行 fn，fn 返回错误则整体回滚。
	WithTx(ctx context.Context, fn func(tx repository.AnnouncementTx) error) error
}

// AnnouncementDeps 是 AnnouncementService 的依赖。
type AnnouncementDeps struct {
	Repo  AnnouncementRepo // 公告表
	Audit AdminAuditWriter // 后台审计，nil 时不写
	Now   func() time.Time // 当前时间，nil 时用 time.Now；测试注入固定时钟
}

// AnnouncementService 首页公告：后台增删改排序，用户端读取生效中的公告。
// 公告全站共享、不属于某个用户，所以不做 user_id 隔离；后台写权限（admin 或 super_admin）由路由保证。
type AnnouncementService struct {
	repo  AnnouncementRepo
	audit AdminAuditWriter
	now   func() time.Time
}

// NewAnnouncementService 创建公告服务。
func NewAnnouncementService(d AnnouncementDeps) *AnnouncementService {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &AnnouncementService{repo: d.Repo, audit: d.Audit, now: now}
}

// Public 返回用户端要轮播的公告：启用、在生效时间内、未删除，按后台排序，最多 5 条；items 永不为 nil。
func (s *AnnouncementService) Public(ctx context.Context) (*model.AnnouncementPublicView, error) {
	// 1. 读全部再在内存里筛：表很小（预计不到 100 行），这样「是否生效」「怎么排」只有 rankLive 一处实现，
	//    后台列表的「展示中 / 超出上限」与用户端看到的永远一致
	rows, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取公告失败：%w", err)
	}
	live := rankLive(rows, s.now())

	// 2. 截到前 5 条，转成只含展示字段的公开视图；version 用 updated_at 毫秒，前端按 id+version 记「已关闭」
	if len(live) > maxPublicAnnouncements {
		live = live[:maxPublicAnnouncements]
	}
	items := make([]model.AnnouncementPublicItem, 0, len(live))
	for _, a := range live {
		items = append(items, model.AnnouncementPublicItem{
			ID: a.ID, Title: a.Title,
			EndsAt: a.EndsAt, Version: a.UpdatedAt.UnixMilli(),
		})
	}
	return &model.AnnouncementPublicView{Items: items}, nil
}

// AdminList 返回后台列表：全部未删除公告（含停用），按排序升序，每条带派生状态；items 永不为 nil。
func (s *AnnouncementService) AdminList(ctx context.Context) (*model.AnnouncementAdminView, error) {
	rows, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取公告失败：%w", err)
	}
	status := statusByID(rows, s.now())
	items := make([]model.AnnouncementAdminItem, 0, len(rows))
	for i := range rows {
		items = append(items, adminAnnouncement(&rows[i], status[rows[i].ID]))
	}
	return &model.AnnouncementAdminView{Items: items}, nil
}

// Create 新建公告并返回它的后台视图；新公告排在最后，写审计。
func (s *AnnouncementService) Create(ctx context.Context, actorID uint64, req *model.SaveAnnouncementReq) (*model.AnnouncementAdminItem, error) {
	// 1. 校验并规整全部字段；不合法直接返回，不写库、不写审计
	f, err := normalizeAnnouncement(req)
	if err != nil {
		return nil, err
	}

	// 2. 新公告排在最后：sort = 当前最大 sort + 1（没有公告时 MaxSort 返回 -1，第一条是 0）。
	//    只有管理员会写，并发新增时 sort 偶尔相同也无妨：读取时 sort 相同按 id 升序，顺序仍然确定
	top, err := s.repo.MaxSort(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取公告排序失败：%w", err)
	}
	a := &model.Announcement{
		Title: f.title, Enabled: f.enabled,
		StartsAt: f.startsAt, EndsAt: f.endsAt, Sort: top + 1, CreatedBy: actorID, UpdatedBy: actorID,
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("保存公告失败：%w", err)
	}

	// 3. 审计：记标题、开关和时间窗口；正文可能很长，不记
	adminAudit(ctx, s.audit, actorID, model.AdminAuditAnnouncementCreate, model.AdminAuditTargetAnnounce, a.ID, auditDetail(a))
	return s.adminItem(ctx, a.ID)
}

// Update 整体替换一条公告（没传的可选字段视为清空）并返回最新的后台视图；排序不变，updated_at 刷新（版本变化，用户已关闭的会重新出现）。
func (s *AnnouncementService) Update(ctx context.Context, actorID, id uint64, req *model.SaveAnnouncementReq) (*model.AnnouncementAdminItem, error) {
	// 1. 校验并规整全部字段
	f, err := normalizeAnnouncement(req)
	if err != nil {
		return nil, err
	}

	// 2. 整体写入；开始 / 结束时间传 nil 表示置 NULL（不限）。仓储返回 ErrNotFound 转成 404 / 56001
	err = s.repo.Update(ctx, id, map[string]any{
		"title": f.title, "enabled": f.enabled,
		"starts_at": f.startsAt, "ends_at": f.endsAt, "updated_by": actorID,
	})
	if err != nil {
		return nil, notFoundAs(err, "更新公告失败")
	}

	// 3. 审计并返回最新视图（状态依赖全表排名，所以重新读）
	item, err := s.adminItem(ctx, id)
	if err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditAnnouncementUpdate, model.AdminAuditTargetAnnounce, id, map[string]any{
		"title": item.Title, "enabled": item.Enabled, "starts_at": item.StartsAt, "ends_at": item.EndsAt,
	})
	return item, nil
}

// SetEnabled 启用或停用一条公告并返回最新的后台视图。只改开关，不刷新 updated_at：
// 启停不是内容变化，停用再启用后，用户已关闭的公告不应重新出现。
func (s *AnnouncementService) SetEnabled(ctx context.Context, actorID, id uint64, enabled bool) (*model.AnnouncementAdminItem, error) {
	if err := s.repo.SetEnabled(ctx, id, enabled, actorID); err != nil {
		return nil, notFoundAs(err, "切换公告启用状态失败")
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditAnnouncementEnable, model.AdminAuditTargetAnnounce, id, map[string]any{"enabled": enabled})
	return s.adminItem(ctx, id)
}

// Delete 软删除一条公告并写审计；不存在返回 404 / 56001。
func (s *AnnouncementService) Delete(ctx context.Context, actorID, id uint64) error {
	// 1. 先读出来留给审计（删了就读不到标题了）；不存在直接 404
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return notFoundAs(err, "读取公告失败")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return notFoundAs(err, "删除公告失败")
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditAnnouncementDelete, model.AdminAuditTargetAnnounce, id, map[string]any{"title": a.Title})
	return nil
}

// Reorder 按 ids 的顺序把全部公告的 sort 重写为 0..n-1；ids 必须恰好包含全部未删除公告各一次，否则 400 / 56003。
// 排序不刷新 updated_at（不是内容变化）。
func (s *AnnouncementService) Reorder(ctx context.Context, actorID uint64, ids []uint64) error {
	// 1. 在一个事务里「加锁读取现有 id → 校验 → 逐条重写 sort」：校验与写入必须在同一把锁内，
	//    否则校验通过后别的管理员新增 / 删除公告，会得到不完整的排序。校验或写入失败时整体回滚
	err := s.repo.WithTx(ctx, func(tx repository.AnnouncementTx) error {
		current, err := tx.ListIDs(ctx)
		if err != nil {
			return err
		}
		if !sameIDSet(current, ids) {
			return errcode.ErrAnnouncementOrderMismatch
		}
		for i, id := range ids {
			if err := tx.SetSort(ctx, id, i); err != nil {
				return fmt.Errorf("更新第 %d 条公告的排序失败：%w", i+1, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 2. 审计：记下新的顺序
	adminAudit(ctx, s.audit, actorID, model.AdminAuditAnnouncementOrder, model.AdminAuditTargetAnnounce, 0, map[string]any{"ids": ids})
	return nil
}

// adminItem 重新读取一条公告并算出它在全表里的派生状态（「超出上限」取决于其他公告，只能整表算）。
func (s *AnnouncementService) adminItem(ctx context.Context, id uint64) (*model.AnnouncementAdminItem, error) {
	rows, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取公告失败：%w", err)
	}
	status := statusByID(rows, s.now())
	for i := range rows {
		if rows[i].ID == id {
			item := adminAnnouncement(&rows[i], status[id])
			return &item, nil
		}
	}
	// 刚写完就被另一个管理员删掉：按不存在处理
	return nil, errcode.ErrAnnouncementNotFound
}

// announcementFields 是校验、规整后的公告字段。
type announcementFields struct {
	title            string
	enabled          bool
	startsAt, endsAt *time.Time
}

// normalizeAnnouncement 校验并规整请求：文字去首尾空白后按字符数限长，结束时间必须晚于开始时间。
func normalizeAnnouncement(req *model.SaveAnnouncementReq) (*announcementFields, error) {
	// 1. enabled 必填（handler 的 binding 已拦一道，这里兜底，避免漏传被当成 false 悄悄停用）
	if req.Enabled == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("启用开关必填")
	}

	// 2. 标题 1–60 个字符，按去首尾空白后的字符数（不是字节数）算，中文和英文一样长
	title := strings.TrimSpace(req.Title)
	if n := utf8.RuneCountInString(title); n < 1 || n > maxAnnouncementTitleRunes {
		return nil, errcode.ErrAnnouncementInvalid.WithMsg(fmt.Sprintf("标题需要 1–%d 个字", maxAnnouncementTitleRunes))
	}

	// 3. 时间窗口：都给时结束必须晚于开始；允许保存已经过期的时间（后台显示「已过期」），不拦截
	if req.StartsAt != nil && req.EndsAt != nil && !req.EndsAt.After(*req.StartsAt) {
		return nil, errcode.ErrAnnouncementInvalid.WithMsg("结束时间要晚于开始时间")
	}
	return &announcementFields{
		title: title, enabled: *req.Enabled,
		startsAt: utcPtr(req.StartsAt), endsAt: utcPtr(req.EndsAt),
	}, nil
}

// utcPtr 把时间统一转成 UTC 再入库，避免同一时刻因时区不同在比较、序列化时看起来不一样。
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// inWindow 判断公告此刻是否在生效期：启用，开始时间已到（含恰好此刻），结束时间未到（恰好此刻算已结束）。
func inWindow(a *model.Announcement, now time.Time) bool {
	return a.Enabled && (a.StartsAt == nil || !a.StartsAt.After(now)) && (a.EndsAt == nil || a.EndsAt.After(now))
}

// rankLive 返回此刻生效的公告，按用户端展示顺序排：sort 升序，再按 id 升序。
func rankLive(rows []model.Announcement, now time.Time) []model.Announcement {
	var live []model.Announcement
	for i := range rows {
		if inWindow(&rows[i], now) {
			live = append(live, rows[i])
		}
	}
	sort.SliceStable(live, func(i, j int) bool {
		if live[i].Sort != live[j].Sort {
			return live[i].Sort < live[j].Sort
		}
		return live[i].ID < live[j].ID
	})
	return live
}

// statusByID 算出每条公告的派生状态。「展示中 / 超出上限」用和 Public 同一个 rankLive 排名，保证后台看到的与用户端一致。
func statusByID(rows []model.Announcement, now time.Time) map[uint64]string {
	out := make(map[uint64]string, len(rows))
	for i := range rows {
		a := &rows[i]
		switch {
		case !a.Enabled:
			out[a.ID] = model.AnnouncementStatusDisabled
		case a.StartsAt != nil && a.StartsAt.After(now):
			out[a.ID] = model.AnnouncementStatusScheduled
		case a.EndsAt != nil && !a.EndsAt.After(now):
			out[a.ID] = model.AnnouncementStatusExpired
		}
	}
	for i, a := range rankLive(rows, now) {
		if i < maxPublicAnnouncements {
			out[a.ID] = model.AnnouncementStatusLive
		} else {
			out[a.ID] = model.AnnouncementStatusOverflow
		}
	}
	return out
}

// adminAnnouncement 把表行转成后台视图。
func adminAnnouncement(a *model.Announcement, status string) model.AnnouncementAdminItem {
	return model.AnnouncementAdminItem{
		ID: a.ID, Title: a.Title, Enabled: a.Enabled,
		StartsAt: a.StartsAt, EndsAt: a.EndsAt, Sort: a.Sort, Status: status, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

// auditDetail 是新建公告时写进审计的字段（不含正文）。
func auditDetail(a *model.Announcement) map[string]any {
	return map[string]any{
		"title": a.Title, "enabled": a.Enabled, "starts_at": a.StartsAt, "ends_at": a.EndsAt,
	}
}

// notFoundAs 把仓储的 ErrNotFound 转成 404 / 56001，其他错误加上下文后原样上抛（由 response.Fail 记日志并返回 500）。
func notFoundAs(err error, action string) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrAnnouncementNotFound
	}
	return fmt.Errorf("%s：%w", action, err)
}

// sameIDSet 判断 ids 是否恰好是 current 这组 id（各出现一次，顺序不限）。
func sameIDSet(current, ids []uint64) bool {
	if len(current) != len(ids) {
		return false
	}
	seen := make(map[uint64]bool, len(current))
	for _, id := range current {
		seen[id] = true
	}
	for _, id := range ids {
		if !seen[id] {
			return false
		}
		delete(seen, id) // 删掉已匹配的，重复的 id 第二次就找不到了
	}
	return true
}
