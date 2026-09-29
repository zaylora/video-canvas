package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// webhookMaxBody 是平台回调体的大小上限：回调只用来取一个任务 id，正常只有几百字节。
const webhookMaxBody = 1 << 20

// GenerationTaskHandler 生成任务、积分与平台回调的 HTTP 接口。
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

// 平台回调（不走 JWT，路径里的密钥即凭证）：只触发对应任务立即查询，不信任回调内容；密钥不对返回 404
func (h *GenerationTaskHandler) Webhook(c *gin.Context) {
	// 回调体读不出来（超限 / 断开）也当作空体处理：service 会忽略无法解析的回调并仍返回 200
	body, _ := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, webhookMaxBody))
	if err := h.svc.HandleWebhook(c.Request.Context(), c.Param("provider"), c.Param("secret"), body); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
