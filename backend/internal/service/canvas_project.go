package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/pkg/pagination"
)

type CanvasProjectRepo interface {
	Create(ctx context.Context, p *model.CanvasProject) error
	GetByID(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
	List(ctx context.Context, userID uint64, keyword string, offset, limit int) ([]model.CanvasProjectItem, int64, error)
	Update(ctx context.Context, userID, id, revision uint64, fields map[string]any) error
	Delete(ctx context.Context, userID, id uint64) error
}

type CanvasProjectService struct {
	repo CanvasProjectRepo
}

func NewCanvasProjectService(repo CanvasProjectRepo) *CanvasProjectService {
	return &CanvasProjectService{repo: repo}
}

// Create 创建画布，payload_json 不传时默认为空对象。
func (s *CanvasProjectService) Create(ctx context.Context, userID uint64, req *model.CreateCanvasProjectReq) (*model.CanvasProject, error) {
	// 1. 校验画布内容：不传时默认为 {}，传了必须是合法的 JSON 对象
	payload := req.PayloadJSON
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	} else if !isJSONObject(payload) {
		return nil, errcode.ErrCanvasPayload
	}

	// 2. 创建画布，归属当前登录用户，版本号从 1 开始
	p := &model.CanvasProject{
		UserID:      userID,
		Title:       req.Title,
		PayloadJSON: datatypes.JSON(payload),
		Revision:    1,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Get 查询画布详情，只能查到当前用户自己的画布。
func (s *CanvasProjectService) Get(ctx context.Context, userID, id uint64) (*model.CanvasProject, error) {
	// 按 id + user_id 查询，别人的画布和不存在的画布统一返回「画布不存在」，避免暴露画布是否存在
	p, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrCanvasNotFound
	}
	return p, err
}

// List 分页查询当前用户的画布列表，返回当前页数据、总数和修正后的分页参数（供响应回显）。
func (s *CanvasProjectService) List(ctx context.Context, userID uint64, req *model.ListCanvasProjectReq) ([]model.CanvasProjectItem, int64, pagination.Query, error) {
	// 1. 校验关键词：非法 UTF-8 直接拼进 SQL 会被 PostgreSQL 拒绝，提前按参数错误返回
	if !utf8.ValidString(req.Keyword) {
		return nil, 0, pagination.Query{}, errcode.ErrInvalidParams.WithMsg("keyword 编码错误")
	}
	// 2. 修正分页参数：page < 1 取 1，page_size < 1 取 10，最大 50
	q := pagination.Query{Page: req.Page, PageSize: req.PageSize}
	q.Normalize()
	// 3. 按标题模糊搜索（转义 % 和 _），按更新时间倒序，只查列表字段不含 payload_json
	items, total, err := s.repo.List(ctx, userID, req.Keyword, q.Offset(), q.Limit())
	return items, total, q, err
}

// Update 带乐观锁更新，成功后返回最新的画布（revision 已 +1）。
func (s *CanvasProjectService) Update(ctx context.Context, userID, id uint64, req *model.UpdateCanvasProjectReq) (*model.CanvasProject, error) {
	// 1. 收集要更新的字段：只更新请求里传了的字段
	fields := map[string]any{}
	if req.Title != nil {
		fields["title"] = *req.Title
	}
	if len(req.PayloadJSON) > 0 {
		if !isJSONObject(req.PayloadJSON) {
			return nil, errcode.ErrCanvasPayload
		}
		fields["payload_json"] = datatypes.JSON(req.PayloadJSON)
	}
	// 2. 一个字段都没传时直接报错，不做空更新
	if len(fields) == 0 {
		return nil, errcode.ErrCanvasNoChange
	}

	// 3. 乐观锁更新：只有 revision 与数据库一致才更新并把 revision +1；
	//    画布不存在返回 404，版本不一致说明已被其他人/其他端修改，返回 409
	switch err := s.repo.Update(ctx, userID, id, req.Revision, fields); {
	case errors.Is(err, repository.ErrNotFound):
		return nil, errcode.ErrCanvasNotFound
	case errors.Is(err, repository.ErrRevisionConflict):
		return nil, errcode.ErrCanvasConflict
	case err != nil:
		return nil, err
	}
	// 4. 重新查询，返回更新后的完整画布（含最新 revision）
	return s.Get(ctx, userID, id)
}

// Delete 删除当前用户的画布（软删除，只写 deleted_at）。
func (s *CanvasProjectService) Delete(ctx context.Context, userID, id uint64) error {
	// 按 id + user_id 删除，删不到（不存在或不属于当前用户）返回「画布不存在」
	err := s.repo.Delete(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrCanvasNotFound
	}
	return err
}

// isJSONObject 判断是否为合法的 JSON 对象（画布内容必须是 {...}）。
func isJSONObject(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && data[0] == '{' && json.Valid(data)
}
