package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminUserHandler 处理后台用户管理接口（/admin/users）。必须挂在 JWTAuth + RequireActive + RequireAdmin 之后。
type AdminUserHandler struct {
	svc *service.AdminUserService
}

// NewAdminUserHandler 创建后台用户 handler。
func NewAdminUserHandler(svc *service.AdminUserService) *AdminUserHandler {
	return &AdminUserHandler{svc: svc}
}

// List 分页查询用户（GET /admin/users）。
func (h *AdminUserHandler) List(c *gin.Context) {
	var req model.ListAdminUserReq
	if !BindQuery(c, &req) {
		return
	}
	items, total, q, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OKPage(c, items, total, q.Page, q.PageSize)
}

// Get 返回用户详情（GET /admin/users/:id）。
func (h *AdminUserHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	detail, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// actorID 返回当前登录的管理员 id（JWTAuth 已校验）。
func actorID(c *gin.Context) uint64 { return uint64(middleware.GetUserID(c)) }

// Tasks 返回用户的生成记录（GET /admin/users/:id/tasks），游标分页。
func (h *AdminUserHandler) Tasks(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.ListUserTasksReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListTasks(c.Request.Context(), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// Ledger 返回用户的积分流水（GET /admin/users/:id/ledger），游标分页。
func (h *AdminUserHandler) Ledger(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.ListUserLedgerReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListLedger(c.Request.Context(), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// Logins 返回用户的登录记录（GET /admin/users/:id/logins），游标分页。
func (h *AdminUserHandler) Logins(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.ListUserLoginsReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListLogins(c.Request.Context(), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// AdjustCredits 调整用户积分（POST /admin/users/:id/credits），返回最新账户快照。
func (h *AdminUserHandler) AdjustCredits(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.AdjustCreditsReq
	if !BindJSON(c, &req) {
		return
	}
	view, err := h.svc.AdjustCredits(c.Request.Context(), actorID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// SetLimit 设置单用户并发上限（PUT /admin/users/:id/limits）。
func (h *AdminUserHandler) SetLimit(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.SetUserLimitReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SetMaxActiveTasks(c.Request.Context(), actorID(c), id, req.MaxActiveTasks); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// SetStatus 封禁 / 启用用户（PUT /admin/users/:id/status）。
func (h *AdminUserHandler) SetStatus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.SetUserStatusReq
	if !BindJSON(c, &req) {
		return
	}
	res, err := h.svc.SetStatus(c.Request.Context(), actorID(c), id, req.Status, req.CancelActive)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// BatchCredits 批量发积分（POST /admin/users/batch/credits），逐项返回结果。
func (h *AdminUserHandler) BatchCredits(c *gin.Context) {
	var req model.BatchCreditsReq
	if !BindJSON(c, &req) {
		return
	}
	res, err := h.svc.BatchAddCredits(c.Request.Context(), actorID(c), req.IDs, req.Amount, req.Note)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// BatchStatus 批量封禁 / 启用（PUT /admin/users/batch/status），逐项返回结果。
func (h *AdminUserHandler) BatchStatus(c *gin.Context) {
	var req model.BatchStatusReq
	if !BindJSON(c, &req) {
		return
	}
	res, err := h.svc.BatchSetStatus(c.Request.Context(), actorID(c), req.IDs, req.Status)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// SetRole 调整用户角色（PUT /admin/users/:id/role，仅 super_admin）。
func (h *AdminUserHandler) SetRole(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.SetUserRoleReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SetRole(c.Request.Context(), actorID(c), id, req.Role); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// ResetPassword 重置用户密码（POST /admin/users/:id/reset-password，仅 super_admin）。
// 请求体可以省略（连空 body 都行）：缺省时生成临时密码；返回的 temp_password 只此一次。
func (h *AdminUserHandler) ResetPassword(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.ResetPasswordReq
	if c.Request.ContentLength != 0 && !BindJSON(c, &req) {
		return
	}
	view, err := h.svc.ResetPassword(c.Request.Context(), actorID(c), id, req.NewPassword)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}
