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

// AdminAIHandler 是 AI 配置的管理接口（平台协议 / 模型 / 凭证 / 试跑 / 导入），
// 必须挂在 JWTAuth + RequireAdmin 之后。
type AdminAIHandler struct {
	svc *service.AIConfigService
}

func NewAdminAIHandler(svc *service.AIConfigService) *AdminAIHandler {
	return &AdminAIHandler{svc: svc}
}

// Register 在管理分组（/api/v1/admin/ai，已挂好鉴权和 RequireAdmin）下注册所有路由。
//
// 路由按 providers / models 两个静态前缀分开注册，而不是用 /:target/:key/... 这种通配写法：
// 通配段与 /secrets、/import 等静态段同层时 Gin 的路由树容易冲突，分开注册最稳妥。
func (h *AdminAIHandler) Register(admin *gin.RouterGroup) {
	for target, path := range map[string]string{model.ConfigTargetProvider: "/providers", model.ConfigTargetModel: "/models"} {
		g := admin.Group(path)
		g.GET("", h.list(target))
		g.POST("", h.create(target))
		g.GET("/:key", h.get(target))
		g.PUT("/:key", h.update(target))
		g.POST("/:key/validate", h.validate(target))
		g.POST("/:key/publish", h.publish(target))
		g.POST("/:key/rollback", h.rollback(target))
		g.GET("/:key/revisions", h.revisions(target))
		g.GET("/:key/revisions/:rid", h.revision(target))
	}
	models := admin.Group("/models")
	models.POST("/:key/dry-run", h.dryRun)
	models.POST("/:key/test-run", h.testRun)
	models.PUT("/:key/enabled", h.setEnabled)
	models.PUT("/:key/sort", h.setSort)

	admin.GET("/test-runs/:id", h.getTestRun)
	admin.POST("/import/runninghub", h.importRunningHub)
	admin.GET("/secrets", h.listSecrets)
	admin.PUT("/secrets/:name", h.setSecret)
	admin.GET("/schema/:target", h.schema)
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
	Input            map[string]any `json:"input" binding:"required" label:"示例输入"`
	UseProviderDraft bool           `json:"use_provider_draft"`
}

type setEnabledReq struct {
	Enabled *bool `json:"enabled" binding:"required" label:"enabled"`
}

type setSortReq struct {
	Sort *int `json:"sort" binding:"required" label:"sort"`
}

type importRunningHubReq struct {
	WebappID string `json:"webapp_id" binding:"required,max=32" label:"webappId"`
	Kind     string `json:"kind" binding:"omitempty,oneof=video image audio" label:"种类"`
	Provider string `json:"provider" binding:"omitempty,max=64" label:"平台"`
}

type secretURI struct {
	Name string `uri:"name" binding:"required,max=128" label:"凭证名"`
}

type setSecretReq struct {
	Value string `json:"value" binding:"required,max=4096" label:"凭证值"`
}

type schemaURI struct {
	Target string `uri:"target" binding:"required,oneof=provider model" label:"target"`
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
// providers / models 共用的接口（按 target 生成）
// ---------------------------------------------------------------------------

// list 列出配置概览（不含正文）。
func (h *AdminAIHandler) list(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := h.svc.ListConfigs(c.Request.Context(), target)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, items)
	}
}

// create 新建配置并保存为草稿；正文有校验问题也会保存，问题列表在响应的 issues 里。
func (h *AdminAIHandler) create(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req saveConfigReq
		if !bindJSON(c, &req) {
			return
		}
		res, err := h.svc.SaveDraft(c.Request.Context(), target, "", true, req.Body, req.Note, currentUserID(c))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, res)
	}
}

// get 读取配置详情：最新草稿与已发布版本的正文及 revision 元信息。
func (h *AdminAIHandler) get(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := pathKey(c)
		if !ok {
			return
		}
		detail, err := h.svc.GetConfig(c.Request.Context(), target, key)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, detail)
	}
}

// update 更新已有配置的草稿。
func (h *AdminAIHandler) update(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := pathKey(c)
		if !ok {
			return
		}
		var req saveConfigReq
		if !bindJSON(c, &req) {
			return
		}
		res, err := h.svc.SaveDraft(c.Request.Context(), target, key, false, req.Body, req.Note, currentUserID(c))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, res)
	}
}

// validate 校验草稿（或请求里传入的正文），返回问题列表。
func (h *AdminAIHandler) validate(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := pathKey(c)
		if !ok {
			return
		}
		var req validateReq
		if !bindOptionalJSON(c, &req) {
			return
		}
		res, err := h.svc.Validate(c.Request.Context(), target, key, req.Body)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, res)
	}
}

// publish 发布最新草稿，成功后立即热生效。
func (h *AdminAIHandler) publish(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := pathKey(c)
		if !ok {
			return
		}
		rev, err := h.svc.Publish(c.Request.Context(), target, key, currentUserID(c))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, rev)
	}
}

// rollback 回滚到指定的历史版本。
func (h *AdminAIHandler) rollback(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := pathKey(c)
		if !ok {
			return
		}
		var req rollbackReq
		if !bindJSON(c, &req) {
			return
		}
		rev, err := h.svc.Rollback(c.Request.Context(), target, key, req.RevisionID, currentUserID(c))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, rev)
	}
}

// revisions 列出历史版本（不含正文）。
func (h *AdminAIHandler) revisions(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := pathKey(c)
		if !ok {
			return
		}
		list, err := h.svc.ListRevisions(c.Request.Context(), target, key)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, list)
	}
}

// revision 读取某个历史版本的完整正文（回滚前预览）。
func (h *AdminAIHandler) revision(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri revisionURI
		if !bindURI(c, &uri) {
			return
		}
		rev, err := h.svc.GetRevision(c.Request.Context(), target, uri.Key, uri.RID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, rev)
	}
}

// ---------------------------------------------------------------------------
// 模型专属接口
// ---------------------------------------------------------------------------

// dryRun 用草稿渲染请求但不发送，返回脱敏后的渲染结果。
func (h *AdminAIHandler) dryRun(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req trialReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.DryRun(c.Request.Context(), key, req.Input, req.UseProviderDraft)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// testRun 用草稿真实跑一次（不扣积分），返回试跑任务视图，前端随后轮询 GET /test-runs/:id。
func (h *AdminAIHandler) testRun(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req trialReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.TestRun(c.Request.Context(), currentUserID(c), key, req.Input, req.UseProviderDraft)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// getTestRun 查询试跑任务的当前状态（只能查自己创建的）。
func (h *AdminAIHandler) getTestRun(c *gin.Context) {
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

// setEnabled 上架 / 下架模型。
func (h *AdminAIHandler) setEnabled(c *gin.Context) {
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

// setSort 修改模型排序值。
func (h *AdminAIHandler) setSort(c *gin.Context) {
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

// importRunningHub 按 webappId 从 RunningHub 导入节点，返回 Model 草稿建议（不落库）。
func (h *AdminAIHandler) importRunningHub(c *gin.Context) {
	var req importRunningHubReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.ImportRunningHub(c.Request.Context(), req.WebappID, req.Kind, req.Provider)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// ---------------------------------------------------------------------------
// 凭证与 JSON Schema
// ---------------------------------------------------------------------------

// listSecrets 列出凭证状态（是否已设置、更新时间、操作人），永远不返回明文或密文。
func (h *AdminAIHandler) listSecrets(c *gin.Context) {
	list, err := h.svc.ListSecrets(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// setSecret 设置（覆盖）凭证。只写：响应里不回显任何凭证内容。
func (h *AdminAIHandler) setSecret(c *gin.Context) {
	var uri secretURI
	if !bindURI(c, &uri) {
		return
	}
	var req setSecretReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.SetSecret(c.Request.Context(), uri.Name, req.Value, currentUserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// schema 返回配置正文的 JSON Schema，供前端 Monaco 编辑器补全。
func (h *AdminAIHandler) schema(c *gin.Context) {
	var uri schemaURI
	if !bindURI(c, &uri) {
		return
	}
	b, err := h.svc.JSONSchema(uri.Target)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, b)
}
