package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// Preflight 接收文件或正文，返回目录和二十四小时来源 token。
func (h *DramaHandler) Preflight(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDramaMultipartBytes)
	var req service.DramaPreflightInput
	if !bindWith(c, c.ShouldBindWith(&req, binding.FormMultipart)) {
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
		files := c.Request.MultipartForm.File["file"]
		if len(files) > 1 {
			response.Fail(c, errcode.ErrInvalidParams.WithMsg("每次只可上传一个原文文件"))
			return
		}
		if len(files) == 1 {
			file, err := files[0].Open()
			if err != nil {
				response.Fail(c, errcode.ErrInvalidParams)
				return
			}
			defer file.Close()
			req.Filename = files[0].Filename
			req.Data, err = io.ReadAll(file)
			if err != nil {
				response.Fail(c, errcode.ErrInvalidParams)
				return
			}
		}
	}
	v, err := h.svc.Preflight(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// EstimateSource 在草稿上重算模型、类型、时长和范围的自动部分估价。
func (h *DramaHandler) EstimateSource(c *gin.Context) {
	var req model.DramaSourceEstimateReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.EstimateSource(c.Request.Context(), currentUserID(c), c.Param("token"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// PromptTemplates 只返回可替换正文，不返回固定协议。
func (h *DramaHandler) PromptTemplates(c *gin.Context) {
	items := []gin.H{}
	labels := map[string]string{"plan": "分集规划", "scripts": "分集剧本", "breakdown": "资产拆解", "asset_prompts": "资产图提示词", "storyboard": "视频提示词"}
	for _, step := range []string{"adapt", "assets", "asset_prompts", "video"} {
		pages := []gin.H{}
		for _, key := range service.DramaPromptPages(step) {
			pages = append(pages, gin.H{"key": key, "label": labels[key], "body": drama.PromptBody(key)})
		}
		items = append(items, gin.H{"step": step, "pages": pages})
	}
	response.OK(c, gin.H{"items": items})
}

// ListPrompts 返回本人提示词，包括可编辑正文。
func (h *DramaHandler) ListPrompts(c *gin.Context) {
	var req struct {
		Step string `form:"step" binding:"omitempty,oneof=adapt assets asset_prompts video"`
	}
	if !BindQuery(c, &req) {
		return
	}
	items, err := h.svc.ListPrompts(c.Request.Context(), currentUserID(c), req.Step)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// CreatePrompt 创建个人可复用提示词正文。
func (h *DramaHandler) CreatePrompt(c *gin.Context) {
	var req model.SaveDramaPromptReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SavePrompt(c.Request.Context(), currentUserID(c), 0, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// UpdatePrompt 带 revision 覆盖本人提示词。
func (h *DramaHandler) UpdatePrompt(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.SaveDramaPromptReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SavePrompt(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeletePrompt 删除本人提示词，不影响项目和任务快照。
func (h *DramaHandler) DeletePrompt(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeletePrompt(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Skills 返回启用且管理员标记本环节的技能。
func (h *DramaHandler) Skills(c *gin.Context) {
	var req struct {
		Step string `form:"step" binding:"required,oneof=adapt assets asset_prompts video"`
	}
	if !BindQuery(c, &req) {
		return
	}
	items, err := h.svc.Skills(c.Request.Context(), req.Step)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// Units 返回原文单元目录。
func (h *DramaHandler) Units(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req struct {
		From int `form:"from" binding:"min=0"`
		To   int `form:"to" binding:"min=0"`
	}
	if !BindQuery(c, &req) {
		return
	}
	if req.To > 0 && req.To < req.From {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	items, err := h.svc.Units(c.Request.Context(), currentUserID(c), id, max(1, req.From), req.To, false)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// Unit 返回指定单元的完整原文。
func (h *DramaHandler) Unit(c *gin.Context) {
	id, no, ok := dramaEpisodePath(c)
	if !ok {
		return
	}
	items, err := h.svc.Units(c.Request.Context(), currentUserID(c), id, no, no, true)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, items[0])
}
