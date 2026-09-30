package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// GenerationTaskHandler 生成任务与积分的 HTTP 接口。
type GenerationTaskHandler struct {
	svc *service.GenerationTaskService
}

func NewGenerationTaskHandler(svc *service.GenerationTaskService) *GenerationTaskHandler {
	return &GenerationTaskHandler{svc: svc}
}

// 提交生成任务：请求头 Idempotency-Key 保证重复提交只创建一个任务；返回 202 + 任务快照
func (h *GenerationTaskHandler) Create(c *gin.Context) {
	var req model.CreateGenerationTaskReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.Create(c.Request.Context(), currentUserID(c), c.GetHeader("Idempotency-Key"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, view)
}

// 查询单个任务
func (h *GenerationTaskHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := h.svc.Get(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// 对账查询：?ids=1,2,3（最多 100 个）或 ?status=active（所有进行中的任务），data 是任务数组
func (h *GenerationTaskHandler) List(c *gin.Context) {
	var req model.ListGenerationTaskReq
	if !bindQuery(c, &req) {
		return
	}
	views, err := h.svc.List(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, views)
}

// 软取消任务：非终态才能取消，取消后退回冻结的积分
func (h *GenerationTaskHandler) Cancel(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := h.svc.Cancel(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// 当前用户的积分：{balance, frozen, available}
func (h *GenerationTaskHandler) Credits(c *gin.Context) {
	credits, err := h.svc.GetCredits(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, credits)
}
