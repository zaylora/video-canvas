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
	"video-canvas/pkg/pagination"
)

// maxDramaMultipartBytes 限制包含 docx 图片等附件的上传体；正文字符上限仍由服务解析后校验。
const maxDramaMultipartBytes = 32 << 20

// DramaHandler 提供短剧项目、分集剧本与持久流水线接口。
type DramaHandler struct{ svc *service.DramaService }

// NewDramaHandler 创建短剧 HTTP 接口。
func NewDramaHandler(svc *service.DramaService) *DramaHandler { return &DramaHandler{svc: svc} }

// Styles 返回内置题材和画风，描述可用于资产提示词生成。
func (h *DramaHandler) Styles(c *gin.Context) { response.OK(c, drama.Styles()) }

// List 按标题搜索并分页返回当前用户的短剧概要。
func (h *DramaHandler) List(c *gin.Context) {
	var req struct {
		pagination.Query
		Keyword string `form:"keyword" binding:"max=255" label:"关键词"`
	}
	if !BindQuery(c, &req) {
		return
	}
	req.Normalize()
	items, total, err := h.svc.List(c.Request.Context(), currentUserID(c), req.Keyword, req.Page, req.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OKPage(c, items, total, req.Page, req.PageSize)
}

// Create 接收原文文件或粘贴文本，建好项目、专属画布和研读任务后返回 201。
func (h *DramaHandler) Create(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDramaMultipartBytes)
	var req model.CreateDramaReq
	if !bindWith(c, c.ShouldBindWith(&req, binding.FormMultipart)) {
		return
	}
	if c.Request.MultipartForm != nil {
		// multipart 解析可能使用临时文件；请求结束后清理，不影响已经读取进内存的正文。
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	if !readDramaFile(c, &req) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// readDramaFile 读取可选的单个原文文件，来源二选一与正文规则交给服务校验。
func readDramaFile(c *gin.Context, req *model.CreateDramaReq) bool {
	form := c.Request.MultipartForm
	if form == nil || len(form.File["file"]) == 0 {
		return true
	}
	files := form.File["file"]
	if len(files) != 1 {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("每次只可上传一个原文文件"))
		return false
	}
	file, err := files[0].Open()
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("无法读取原文文件"))
		return false
	}
	defer file.Close()
	req.Filename = files[0].Filename
	req.Data, err = io.ReadAll(file)
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("无法读取原文文件"))
		return false
	}
	return true
}

// Get 返回项目设置、大纲、集摘要和进行中的任务。
func (h *DramaHandler) Get(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update 带 revision 更新项目，冲突沿用画布的 409 错误码。
func (h *DramaHandler) Update(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.UpdateDramaReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Delete 软删项目并连带清理专属画布和角色卡，成功返回空正文。
func (h *DramaHandler) Delete(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// GetEpisode 按集号返回单集正文。
func (h *DramaHandler) GetEpisode(c *gin.Context) {
	id, no, ok := dramaEpisodePath(c)
	if !ok {
		return
	}
	v, err := h.svc.GetEpisode(c.Request.Context(), currentUserID(c), id, no)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// CreateEpisode 在项目末尾追加一集，返回 201。
func (h *DramaHandler) CreateEpisode(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CreateDramaEpisodeReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateEpisode(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// UpdateEpisode 带版本号更新一集，锁定集的正文和梗概由服务拒绝修改。
func (h *DramaHandler) UpdateEpisode(c *gin.Context) {
	id, no, ok := dramaEpisodePath(c)
	if !ok {
		return
	}
	var req model.UpdateDramaEpisodeReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateEpisode(c.Request.Context(), currentUserID(c), id, no, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteEpisode 删除一集，后面的集号往前补位，返回 204。
func (h *DramaHandler) DeleteEpisode(c *gin.Context) {
	id, no, ok := dramaEpisodePath(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteEpisode(c.Request.Context(), currentUserID(c), id, no); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// dramaEpisodePath 解析十六进制项目 ID 和从 1 起的集号。
func dramaEpisodePath(c *gin.Context) (uint64, int, bool) {
	id, ok := CanvasPathID(c)
	if !ok {
		return 0, 0, false
	}
	var req struct {
		No int `uri:"no" binding:"required,min=1" label:"集号"`
	}
	if !bindURI(c, &req) {
		return 0, 0, false
	}
	return id, req.No, true
}
