package handler

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminAIHandler 是 AI 模型配置的管理接口（草稿 / 发布 / 试跑 / 追踪），
// 必须挂在 JWTAuth + RequireAdmin 之后。插件与渠道见 AdminPluginHandler、AdminChannelHandler。
type AdminAIHandler struct {
	svc *service.AIConfigService
}

func NewAdminAIHandler(svc *service.AIConfigService) *AdminAIHandler {
	return &AdminAIHandler{svc: svc}
}

// ---------------------------------------------------------------------------
// 请求结构
// ---------------------------------------------------------------------------

// keyURI 是 /:key 路径参数。
type keyURI struct {
	Key string `uri:"key" binding:"required,max=128" label:"key"`
}

// revisionURI 是 /:key/revisions/:rid 路径参数。
type revisionURI struct {
	Key string `uri:"key" binding:"required,max=128" label:"key"`
	RID uint64 `uri:"rid" binding:"required,min=1" label:"revision id"`
}

// saveConfigReq 保存草稿：body 是配置正文（JSON 对象），note 是本次修改说明。
type saveConfigReq struct {
	Body json.RawMessage `json:"body" binding:"required" label:"配置正文"`
	Note string          `json:"note" binding:"max=255" label:"备注"`
}

// validateReq 校验请求：body 可选，不传则校验已保存的最新草稿。
type validateReq struct {
	Body json.RawMessage `json:"body"`
}

// rollbackReq 回滚请求。
type rollbackReq struct {
	RevisionID uint64 `json:"revision_id" binding:"required,min=1" label:"revision_id"`
}

// trialReq 是 dry-run / 试跑请求：input 是示例输入。
type trialReq struct {
	Input map[string]any `json:"input" binding:"required" label:"示例输入"`
}

type setSecretReq struct {
	Value string `json:"value" binding:"required,max=4096" label:"Key"`
}

type setEnabledReq struct {
	Enabled *bool `json:"enabled" binding:"required" label:"enabled"`
}

type setSortReq struct {
	Sort *int `json:"sort" binding:"required" label:"sort"`
}

// bindOptionalJSON 绑定可选的 JSON 请求体：完全没有请求体时视为空请求，不报错。
func bindOptionalJSON(c *gin.Context, req any) bool {
	if c.Request.ContentLength == 0 {
		return true
	}
	err := c.ShouldBindJSON(req)
	if errors.Is(err, io.EOF) {
		return true
	}
	return bindWith(c, err)
}

// pathKey 取路径参数 :key。
func pathKey(c *gin.Context) (string, bool) {
	var uri keyURI
	if !bindURI(c, &uri) {
		return "", false
	}
	return uri.Key, true
}

// ---------------------------------------------------------------------------
// 模型配置接口
// ---------------------------------------------------------------------------

// List 列出配置概览（不含正文）。
func (h *AdminAIHandler) List(c *gin.Context) {
	items, err := h.svc.ListConfigs(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, items)
}

// Create 新建配置并保存为草稿；正文有校验问题也会保存，问题列表在响应的 issues 里。
func (h *AdminAIHandler) Create(c *gin.Context) {
	var req saveConfigReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.SaveDraft(c.Request.Context(), service.ModelDraftInput{
		Create: true, Body: req.Body, Note: req.Note, AdminID: currentUserID(c),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Get 读取配置详情：最新草稿与已发布版本的正文及 revision 元信息。
func (h *AdminAIHandler) Get(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	detail, err := h.svc.GetConfig(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// Update 更新已有配置的草稿。
func (h *AdminAIHandler) Update(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req saveConfigReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.SaveDraft(c.Request.Context(), service.ModelDraftInput{
		Key: key, Body: req.Body, Note: req.Note, AdminID: currentUserID(c),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Validate 校验草稿（或请求里传入的正文），返回问题列表。
func (h *AdminAIHandler) Validate(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req validateReq
	if !bindOptionalJSON(c, &req) {
		return
	}
	res, err := h.svc.Validate(c.Request.Context(), key, req.Body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Publish 发布最新草稿，成功后立即热生效。
func (h *AdminAIHandler) Publish(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	rev, err := h.svc.Publish(c.Request.Context(), key, currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, rev)
}

// Rollback 回滚到指定的历史版本。
func (h *AdminAIHandler) Rollback(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req rollbackReq
	if !bindJSON(c, &req) {
		return
	}
	rev, err := h.svc.Rollback(c.Request.Context(), key, req.RevisionID, currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, rev)
}

// Revisions 列出历史版本（不含正文）。
func (h *AdminAIHandler) Revisions(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	list, err := h.svc.ListRevisions(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Revision 读取某个历史版本的完整正文（回滚前预览）。
func (h *AdminAIHandler) Revision(c *gin.Context) {
	var uri revisionURI
	if !bindURI(c, &uri) {
		return
	}
	rev, err := h.svc.GetRevision(c.Request.Context(), uri.Key, uri.RID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, rev)
}

// ---------------------------------------------------------------------------
// 模型专属接口
// ---------------------------------------------------------------------------

// DryRun 用草稿渲染请求但不发送，返回脱敏后的渲染结果。
func (h *AdminAIHandler) DryRun(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req trialReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.DryRun(c.Request.Context(), key, req.Input)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// TestRun 用草稿真实跑一次（不扣积分），返回试跑任务视图，前端随后轮询 GET /test-runs/:id。
func (h *AdminAIHandler) TestRun(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req trialReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.TestRun(c.Request.Context(), currentUserID(c), key, req.Input)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// GetTestRun 查询试跑任务的当前状态（只能查自己创建的）。
func (h *AdminAIHandler) GetTestRun(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := h.svc.GetTestRun(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// SetEnabled 上架 / 下架模型。
func (h *AdminAIHandler) SetEnabled(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req setEnabledReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.SetModelEnabled(c.Request.Context(), key, *req.Enabled, currentUserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// SetSort 修改模型排序值。
func (h *AdminAIHandler) SetSort(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req setSortReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.SetModelSort(c.Request.Context(), key, *req.Sort, currentUserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// ---------------------------------------------------------------------------
// 试跑追踪与 JSON Schema
// ---------------------------------------------------------------------------

// GetTestTrace 查询试跑任务的执行追踪（每次钩子与 HTTP 的输入输出，已脱敏；只能查自己创建的试跑任务）。
func (h *AdminAIHandler) GetTestTrace(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	trace, err := h.svc.GetTestTrace(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, trace)
}

// Schema 返回模型配置正文的 JSON Schema，供前端 Monaco 编辑器补全（路由固定为 /schema/model）。
func (h *AdminAIHandler) Schema(c *gin.Context) {
	b, err := h.svc.JSONSchema(model.ConfigTargetModel)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, b)
}

// DeleteCheck 删除模型前的预检：返回阻断删除的原因，blockers 为空数组表示可以删。
func (h *AdminAIHandler) DeleteCheck(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	res, err := h.svc.CheckModelDelete(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Delete 删除一个已下架的模型（连同它的全部版本，不可恢复）。
func (h *AdminAIHandler) Delete(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteModel(c.Request.Context(), key, currentUserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
