package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// maxCharacterIDs 是 GET /characters?ids= 一次最多查询的 ID 数（与契约一致）。
const maxCharacterIDs = 100

// CharacterCardHandler 角色卡接口：ID 是十六进制串，素材 ID 是数字。
type CharacterCardHandler struct {
	svc *service.CharacterCardService
}

// NewCharacterCardHandler 创建角色卡接口。
func NewCharacterCardHandler(svc *service.CharacterCardService) *CharacterCardHandler {
	return &CharacterCardHandler{svc: svc}
}

// List 默认返回个人角色卡；?ids= 批量读取包含短剧角色（有 ids 时忽略 q），?q= 按名称搜索。
func (h *CharacterCardHandler) List(c *gin.Context) {
	userID := currentUserID(c)
	if raw, hasIDs := c.GetQuery("ids"); hasIDs {
		ids, err := parseCharacterIDs(raw)
		if err != nil {
			response.Fail(c, err)
			return
		}
		v, err := h.svc.ListByIDs(c.Request.Context(), userID, ids)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, v)
		return
	}
	v, err := h.svc.List(c.Request.Context(), userID, c.Query("q"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Get 角色卡详情。
func (h *CharacterCardHandler) Get(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Create 新建角色卡，成功返回 201 和角色卡视图。
func (h *CharacterCardHandler) Create(c *gin.Context) {
	var req model.CreateCharacterReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// Update 部分更新角色卡：没传不改，主图和声音传 null 清空，补充图传 [] 清空，可更新造型名和音色描述。
func (h *CharacterCardHandler) Update(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.UpdateCharacterReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Delete 删除角色卡（不删素材），成功返回 204 和空正文。
func (h *CharacterCardHandler) Delete(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// parseCharacterIDs 把逗号分隔的十六进制 ID 还原成主键；空串是空列表，超过 100 个或格式不对返回参数错误。
func parseCharacterIDs(raw string) ([]uint64, error) {
	if raw == "" {
		return []uint64{}, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxCharacterIDs {
		return nil, errcode.ErrInvalidParams.WithMsg("ids 最多 100 个")
	}
	ids := make([]uint64, 0, len(parts))
	for _, part := range parts {
		id, ok := idcodec.Decode(strings.TrimSpace(part))
		if !ok {
			return nil, errcode.ErrInvalidParams.WithMsg("ids 格式错误")
		}
		ids = append(ids, id)
	}
	return ids, nil
}
