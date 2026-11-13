package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
	"video-canvas/pkg/pagination"
)

// MarketingHandler 是营销工作室的用户端接口：项目与轮次、拆解与脚本编辑、出片、发到画布。
// 项目、轮次、脚本、环节运行的 id 都是十六进制串；别人的资源一律按「不存在」处理。
type MarketingHandler struct{ svc *service.MarketingService }

// NewMarketingHandler 创建营销 HTTP 接口。
func NewMarketingHandler(svc *service.MarketingService) *MarketingHandler {
	return &MarketingHandler{svc: svc}
}

// Options 返回平台与角度预设（GET /marketing/options）。
func (h *MarketingHandler) Options(c *gin.Context) { response.OK(c, h.svc.Options()) }

// Quote 预估写脚本积分（POST /marketing/quote）。
func (h *MarketingHandler) Quote(c *gin.Context) {
	var req model.MarketingQuoteReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Quote(c.Request.Context(), currentUserID(c), &req)
	reply(c, v, err)
}

// List 按标题搜索并分页返回项目列表（GET /marketing-projects）。
func (h *MarketingHandler) List(c *gin.Context) {
	var req struct {
		pagination.Query
		Keyword string `form:"keyword" binding:"max=255" label:"关键词"`
	}
	if !BindQuery(c, &req) {
		return
	}
	req.Normalize()
	items, total, err := h.svc.ListProjects(c.Request.Context(), currentUserID(c), req.Keyword, req.Page, req.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OKPage(c, items, total, req.Page, req.PageSize)
}

// Create 创建项目并开第一轮，返回 201（POST /marketing-projects）。
func (h *MarketingHandler) Create(c *gin.Context) {
	var req model.CreateMarketingProjectReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateProject(c.Request.Context(), currentUserID(c), &req)
	created(c, v, err)
}

// Get 返回项目详情（GET /marketing-projects/:id）。
func (h *MarketingHandler) Get(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.GetProject(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// Update 改标题、事实卡或商品图（PUT /marketing-projects/:id）。
func (h *MarketingHandler) Update(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.UpdateMarketingProjectReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateProject(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// Delete 删除项目，返回 204（DELETE /marketing-projects/:id）。
func (h *MarketingHandler) Delete(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteProject(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// NewRound 开新一轮，返回 201（POST /marketing-projects/:id/rounds）。
func (h *MarketingHandler) NewRound(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CreateMarketingRoundReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.NewRound(c.Request.Context(), currentUserID(c), id, &req)
	created(c, v, err)
}

// CancelRound 取消写脚本（POST /marketing-rounds/:id/cancel）。
func (h *MarketingHandler) CancelRound(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.CancelRound(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// UpdateReverse 保存拆解修改（PUT /marketing-rounds/:id/reverse）。
func (h *MarketingHandler) UpdateReverse(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.UpdateMarketingReverseReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateReverse(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// RestoreReverse 拆解恢复原稿（POST /marketing-rounds/:id/reverse/restore）。
func (h *MarketingHandler) RestoreReverse(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.RestoreReverse(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// UpdateRoundSettings 改一轮的出片设置：视频模型、分辨率、换模特图（PUT /marketing-rounds/:id/settings）。
func (h *MarketingHandler) UpdateRoundSettings(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.UpdateMarketingRoundSettingsReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateRoundSettings(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// RefreshFacts 按当前商品图重新识别外观锚点（POST /marketing-projects/:id/refresh-facts）。
func (h *MarketingHandler) RefreshFacts(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.RefreshFacts(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// Refollow 按当前拆解重写跟拍版（POST /marketing-rounds/:id/refollow）。
func (h *MarketingHandler) Refollow(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Refollow(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// RenderRound 全部出片（POST /marketing-rounds/:id/render）。
func (h *MarketingHandler) RenderRound(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.RenderMarketingRoundReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.RenderRound(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// RetryStep 重试失败的环节（POST /marketing-step-runs/:id/retry）。
func (h *MarketingHandler) RetryStep(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.RetryStep(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// UpdateScript 保存旁白修改（PUT /marketing-scripts/:id）。
func (h *MarketingHandler) UpdateScript(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.UpdateMarketingScriptReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateScript(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// RestoreScript 脚本恢复原稿（POST /marketing-scripts/:id/restore）。
func (h *MarketingHandler) RestoreScript(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.RestoreScript(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// Prompt 返回提示词预览（GET /marketing-scripts/:id/prompt?video_model=）。
func (h *MarketingHandler) Prompt(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req struct {
		VideoModel string `form:"video_model" binding:"max=128" label:"视频模型"`
	}
	if !BindQuery(c, &req) {
		return
	}
	v, err := h.svc.Prompt(c.Request.Context(), currentUserID(c), id, req.VideoModel)
	reply(c, v, err)
}

// Adapt 手动转写出片提示词（POST /marketing-scripts/:id/adapt）。
func (h *MarketingHandler) Adapt(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.AdaptMarketingScriptReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Adapt(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// EditAdaptation 手改转写稿（PUT /marketing-scripts/:id/adaptation）。
func (h *MarketingHandler) EditAdaptation(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.EditMarketingAdaptationReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.EditAdaptation(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// EditPrompt 手改出片原稿（PUT /marketing-scripts/:id/prompt）。
func (h *MarketingHandler) EditPrompt(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.EditMarketingPromptReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.EditPrompt(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// RestorePrompt 还原出片原稿，回到按脚本拼出的提示词（DELETE /marketing-scripts/:id/prompt?format=）。
func (h *MarketingHandler) RestorePrompt(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.RestoreMarketingPromptReq
	if !BindQuery(c, &req) {
		return
	}
	v, err := h.svc.RestorePrompt(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// Render 单份出片（POST /marketing-scripts/:id/render）。
func (h *MarketingHandler) Render(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.RenderMarketingScriptReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Render(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, v, err)
}

// ToCanvas 把脚本发到新画布（POST /marketing-scripts/:id/to-canvas）。
func (h *MarketingHandler) ToCanvas(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.ToCanvas(c.Request.Context(), currentUserID(c), id)
	reply(c, v, err)
}

// reply 统一写出 200 响应或业务错误。
func reply(c *gin.Context, v any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// created 统一写出 201 响应或业务错误。
func created(c *gin.Context, v any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}
