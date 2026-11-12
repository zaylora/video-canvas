package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminStepHandler 是后台「环节技能」页的接口：环节列表与详情、发起与查询试跑、设为生效。
// 读写都是 admin 或 super_admin（由路由挂载保证，同技能管理）。
type AdminStepHandler struct {
	svc *service.StepAdminService
}

// NewAdminStepHandler 创建环节后台 handler。
func NewAdminStepHandler(svc *service.StepAdminService) *AdminStepHandler {
	return &AdminStepHandler{svc: svc}
}

// stepKeyURI 是路径里的环节 key，如 marketing.facts。
type stepKeyURI struct {
	Key string `uri:"key" binding:"required,max=64" label:"环节"`
}

// List 返回环节列表（GET /admin/steps）。
func (h *AdminStepHandler) List(c *gin.Context) {
	v, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Get 返回环节详情（GET /admin/steps/:key）：协议、绑定历史、样例与候选方法。
func (h *AdminStepHandler) Get(c *gin.Context) {
	var uri stepKeyURI
	if !bindURI(c, &uri) {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), uri.Key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// CreateTrials 发起试跑（POST /admin/steps/:key/trials），返回 202 和 1 或 2 条排队中的试跑。
func (h *AdminStepHandler) CreateTrials(c *gin.Context) {
	var uri stepKeyURI
	if !bindURI(c, &uri) {
		return
	}
	var req model.CreateStepTrialReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateTrials(c.Request.Context(), currentUserID(c), uri.Key, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, v)
}

// GetTrial 返回试跑结果（GET /admin/step-trials/:id），前端轮询它。
func (h *AdminStepHandler) GetTrial(c *gin.Context) {
	var uri struct {
		ID string `uri:"id" binding:"required,len=32" label:"试跑"`
	}
	if !bindURI(c, &uri) {
		return
	}
	v, err := h.svc.GetTrial(c.Request.Context(), uri.ID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SetBinding 把环节设为候选方法（PUT /admin/steps/:key/binding），返回最新详情。
func (h *AdminStepHandler) SetBinding(c *gin.Context) {
	var uri stepKeyURI
	if !bindURI(c, &uri) {
		return
	}
	var req model.SetStepBindingReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SetBinding(c.Request.Context(), currentUserID(c), uri.Key, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
