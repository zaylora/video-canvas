package agent

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	agentsvc "video-canvas/internal/service/agent"
	"video-canvas/pkg/pagination"
)

// TraceAdminAPI 是后台轨迹接口依赖的业务，由 *agentsvc.TraceAdminService 实现。
type TraceAdminAPI interface {
	// ListRuns 分页列出运行。
	ListRuns(ctx context.Context, req *model.ListAgentRunsReq) (*pagination.Result[agentsvc.AdminRunItem], error)
	// ExportTrace 生成一轮运行的轨迹包，并在取数据之前写审计。
	ExportTrace(ctx context.Context, actorID, runID uint64, format string) (*agentsvc.TraceBundle, error)
}

// 轨迹包的两种下载格式。
const (
	traceFormatZip  = "zip"  // 默认：浏览器下载
	traceFormatJSON = "json" // 命令行用：{files: {文件名: 内容}}，Node 不必为解 zip 加依赖
)

// TraceAdminHandler 处理后台「日志管理 → Agent 运行」的接口（/admin/agent/runs）。必须挂在 JWTAuth + RequireActive + RequireAdmin 之后。
type TraceAdminHandler struct {
	svc TraceAdminAPI
}

// NewTraceAdminHandler 创建后台轨迹 handler。
func NewTraceAdminHandler(svc TraceAdminAPI) *TraceAdminHandler { return &TraceAdminHandler{svc: svc} }

// List 返回运行列表（GET /admin/agent/runs），页码分页。
func (h *TraceAdminHandler) List(c *gin.Context) {
	var req model.ListAgentRunsReq
	if !handler.BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListRuns(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// Export 下载一轮运行的轨迹包（GET /admin/agent/runs/:rid/trace）：默认 zip 附件，?format=json 返回常规信封。
func (h *TraceAdminHandler) Export(c *gin.Context) {
	runID, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	format := c.DefaultQuery("format", traceFormatZip)
	if format != traceFormatZip && format != traceFormatJSON {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("format 只能是 zip 或 json"))
		return
	}
	bundle, err := h.svc.ExportTrace(c.Request.Context(), uint64(middleware.GetUserID(c)), runID, format)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if format == traceFormatJSON {
		response.OK(c, gin.H{"files": bundle.TextFiles()})
		return
	}
	data, err := bundle.Zip()
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="agent-trace-`+c.Param("rid")+`.zip"`)
	c.Data(http.StatusOK, "application/zip", data)
}
