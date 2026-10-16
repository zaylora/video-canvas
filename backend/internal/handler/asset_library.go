package handler

import (
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AssetLibraryHandler 个人资产库与生成历史接口。文件夹和库条目的 id 都是十六进制串，素材 id 沿用数字。
type AssetLibraryHandler struct {
	svc *service.AssetLibraryService
}

// NewAssetLibraryHandler 创建资产库接口。
func NewAssetLibraryHandler(svc *service.AssetLibraryService) *AssetLibraryHandler {
	return &AssetLibraryHandler{svc: svc}
}

// ListGenerated 生成历史：?kind=（必填）&source=&canvas_id=&created_from=&order=&q=&cursor=&limit=
func (h *AssetLibraryHandler) ListGenerated(c *gin.Context) {
	var req model.ListGeneratedAssetsReq
	if !BindQuery(c, &req) {
		return
	}
	in := service.GeneratedListInput{
		Kind: req.Kind, Source: req.Source, Asc: req.Order == "asc", Q: req.Q, Cursor: req.Cursor, Limit: req.Limit,
	}
	if req.CanvasID != "" {
		id, ok := idcodec.Decode(req.CanvasID)
		if !ok {
			response.Fail(c, errcode.ErrInvalidParams.WithMsg("canvas_id 格式错误"))
			return
		}
		in.CanvasID = id
	}
	if req.CreatedFrom != "" {
		t, err := time.Parse(time.RFC3339, req.CreatedFrom)
		if err != nil {
			response.Fail(c, errcode.ErrInvalidParams.WithMsg("created_from 必须是 RFC3339 时间"))
			return
		}
		in.CreatedFrom = &t
	}
	page, err := h.svc.ListGenerated(c.Request.Context(), currentUserID(c), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// ListFolders 当前用户的全部文件夹（不分页）
func (h *AssetLibraryHandler) ListFolders(c *gin.Context) {
	v, err := h.svc.ListFolders(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// CreateFolder 新建文件夹，201
func (h *AssetLibraryHandler) CreateFolder(c *gin.Context) {
	var req model.CreateAssetFolderReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateFolder(c.Request.Context(), currentUserID(c), req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// RenameFolder 重命名文件夹
func (h *AssetLibraryHandler) RenameFolder(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.RenameAssetFolderReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.RenameFolder(c.Request.Context(), currentUserID(c), id, req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteFolder 删除文件夹及其中的库归属，返回 {removed}
func (h *AssetLibraryHandler) DeleteFolder(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.DeleteFolder(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// ListItems 资产库条目：?folder_id=root|<id>|all&kind=&q=&cursor=&limit=
func (h *AssetLibraryHandler) ListItems(c *gin.Context) {
	var req model.ListAssetLibraryItemsReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListItems(c.Request.Context(), currentUserID(c), service.LibraryListInput{
		FolderID: req.FolderID, Kind: req.Kind, Q: req.Q, Cursor: req.Cursor, Limit: req.Limit,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// AddItem 把素材加入资产库：新增返回 201，已在库里返回 200 和现有归属
func (h *AssetLibraryHandler) AddItem(c *gin.Context) {
	var req model.AddAssetLibraryItemReq
	if !BindJSON(c, &req) {
		return
	}
	v, created, err := h.svc.AddItem(c.Request.Context(), currentUserID(c), req.AssetID, uint64(req.FolderID))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if created {
		response.Created(c, v)
		return
	}
	response.OK(c, v)
}

// MoveItem 移动库条目到另一个文件夹（folder_id 为 null 表示根目录）
func (h *AssetLibraryHandler) MoveItem(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.MoveAssetLibraryItemReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.MoveItem(c.Request.Context(), currentUserID(c), id, uint64(req.FolderID))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// RemoveItem 移出资产库（不删素材）
func (h *AssetLibraryHandler) RemoveItem(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.RemoveItem(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
