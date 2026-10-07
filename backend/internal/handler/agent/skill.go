package agent

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/agent/skillpkg"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	agentsvc "video-canvas/internal/service/agent"
)

// SkillAPI 是技能管理接口依赖的业务，由 *agentsvc.SkillService 实现；声明成接口是为了 handler 测试不必搭存储。
type SkillAPI interface {
	// List 返回管理页列表（内置 + 导入），q 按名字/显示名/说明过滤，status 为 enabled / disabled。
	List(ctx context.Context, q, status string) ([]agentsvc.SkillItem, error)
	// Get 返回技能详情。
	Get(ctx context.Context, name string) (*agentsvc.SkillDetail, error)
	// Version 返回某版本的完整信息。
	Version(ctx context.Context, name string, version int) (*agentsvc.SkillVersionView, error)
	// File 读某版本包内的一个文件。
	File(ctx context.Context, name string, version int, path string) (*agentsvc.SkillFileContent, error)
	// Download 返回某版本的整包和文件名。
	Download(ctx context.Context, name string, version int) ([]byte, string, error)
	// Import 上传并预检。
	Import(ctx context.Context, actorID uint64, in agentsvc.SkillImportInput) (*agentsvc.SkillImportView, error)
	// ImportFile 预览暂存包里的一个文件。
	ImportFile(ctx context.Context, actorID uint64, id, path string) (*agentsvc.SkillFileContent, error)
	// DiscardImport 放弃暂存。
	DiscardImport(ctx context.Context, actorID uint64, id string) error
	// ConfirmImport 确认入库。
	ConfirmImport(ctx context.Context, actorID uint64, id string) (*agentsvc.SkillItem, error)
	// SetEnabled 启用 / 停用。
	SetEnabled(ctx context.Context, actorID uint64, name string, enabled bool) (*agentsvc.SkillItem, error)
	// SetActiveVersion 设置生效版本。
	SetActiveVersion(ctx context.Context, actorID uint64, name string, version int) (*agentsvc.SkillItem, error)
	// Rename 改显示名。
	Rename(ctx context.Context, actorID uint64, name, title string) (*agentsvc.SkillItem, error)
	// DeleteCheck 删除预检。
	DeleteCheck(ctx context.Context, name string) (*agentsvc.SkillDeleteCheck, error)
	// DeleteVersion 删除版本。
	DeleteVersion(ctx context.Context, actorID uint64, name string, version int) error
	// DeleteSkill 删除技能。
	DeleteSkill(ctx context.Context, actorID uint64, name string) error
	// Enabled 返回用户端可见的目录（已启用的内置 + 导入技能）。
	Enabled(ctx context.Context) ([]agentsvc.SkillBrief, error)
}

// SkillHandler 是 Agent 技能的 HTTP 接口：管理端（RequireAdmin）加用户端的目录。
type SkillHandler struct {
	svc SkillAPI
}

// NewSkillHandler 创建 handler。
func NewSkillHandler(svc SkillAPI) *SkillHandler { return &SkillHandler{svc: svc} }

// importBodyLimit 是导入请求体的总上限：文件夹导入的解压后总量上限加上 multipart 边界的余量。
const importBodyLimit = skillpkg.MaxTotalBytes + 1<<20

func actorID(c *gin.Context) uint64 { return uint64(middleware.GetUserID(c)) }

// skillName 取路径里的技能名；这里不校验格式，不存在统一返回 61004，不给探测格式的机会。
func skillName(c *gin.Context) string { return c.Param("name") }

// versionParam 解析路径里的版本号，必须是正整数。
func versionParam(c *gin.Context) (int, bool) {
	v, err := strconv.Atoi(c.Param("v"))
	if err != nil || v < 1 {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("版本号格式错误"))
		return 0, false
	}
	return v, true
}

// ---- 用户端 ----

// UserList 返回用户端目录：已启用的技能（含内置）的名字、显示名、说明。
func (h *SkillHandler) UserList(c *gin.Context) {
	list, err := h.svc.Enabled(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// ---- 管理端：读 ----

type listQuery struct {
	Q      string `form:"q" binding:"max=100" label:"关键词"`
	Status string `form:"status" binding:"omitempty,oneof=enabled disabled" label:"状态"`
}

// List 返回技能列表。
func (h *SkillHandler) List(c *gin.Context) {
	var q listQuery
	if !handler.BindQuery(c, &q) {
		return
	}
	list, err := h.svc.List(c.Request.Context(), q.Q, q.Status)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get 返回技能详情。
func (h *SkillHandler) Get(c *gin.Context) {
	d, err := h.svc.Get(c.Request.Context(), skillName(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

// Version 返回某版本的清单、问题和正文。
func (h *SkillHandler) Version(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	res, err := h.svc.Version(c.Request.Context(), skillName(c), v)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

type fileQuery struct {
	Path string `form:"path" binding:"required,max=512" label:"路径"`
}

// File 返回某版本包内的一个文本文件；二进制返回 {binary:true,size}。
func (h *SkillHandler) File(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	var q fileQuery
	if !handler.BindQuery(c, &q) {
		return
	}
	res, err := h.svc.File(c.Request.Context(), skillName(c), v, q.Path)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Download 下载某版本的规范化整包。管理员只能经这个接口取包，不暴露对象存储地址。
func (h *SkillHandler) Download(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	data, name, err := h.svc.Download(c.Request.Context(), skillName(c), v)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	c.Data(http.StatusOK, "application/zip", data)
}

// DeleteCheck 删除技能前的预检。
func (h *SkillHandler) DeleteCheck(c *gin.Context) {
	res, err := h.svc.DeleteCheck(c.Request.Context(), skillName(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// ---- 管理端：导入 ----

// Import 上传并预检（multipart）：字段 file 是 zip；或 files + paths 成对出现，是文件夹 / 单个 SKILL.md 展开的文件。
// 预检不通过也返回 200，问题在 issues 里。用流式读取，内容只在内存里，不落临时文件。
func (h *SkillHandler) Import(c *gin.Context) {
	in, err := readImportForm(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	res, err := h.svc.Import(c.Request.Context(), actorID(c), *in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// readImportForm 流式读取 multipart：zip 超过 MaxZipBytes、总量超过上限返回 413，其余格式问题返回 10001。
func readImportForm(c *gin.Context) (*agentsvc.SkillImportInput, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, importBodyLimit)
	mr, err := c.Request.MultipartReader()
	if err != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("请以 multipart/form-data 上传（zip 用字段 file；文件夹用 files 与 paths）")
	}
	in := &agentsvc.SkillImportInput{}
	var paths []string
	var contents [][]byte
	var total int64
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, uploadErr(err)
		}
		data, err := readPart(part)
		if err != nil {
			return nil, err
		}
		if total += int64(len(data)); total > skillpkg.MaxTotalBytes {
			return nil, errcode.ErrSkillTooLarge
		}
		switch part.FormName() {
		case "file":
			in.Zip = data
		case "paths":
			paths = append(paths, string(data))
		case "files":
			contents = append(contents, data)
		}
	}
	if len(paths) != len(contents) {
		return nil, errcode.ErrInvalidParams.WithMsg("files 与 paths 必须成对出现")
	}
	if len(in.Zip) > 0 && len(contents) > 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("zip 与文件夹不能同时上传")
	}
	for i := range paths {
		in.Files = append(in.Files, skillpkg.Input{Path: paths[i], Data: contents[i]})
	}
	if len(in.Zip) == 0 && len(in.Files) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("请上传 zip 压缩包、文件夹或 SKILL.md")
	}
	return in, nil
}

// readPart 读出一个表单字段：zip 最多 MaxZipBytes，其余（文件夹里的单个文件、路径）最多 MaxFileBytes，超出返回 413。
func readPart(part *multipart.Part) ([]byte, error) {
	limit := int64(skillpkg.MaxFileBytes) + 1
	if part.FormName() == "file" {
		limit = int64(skillpkg.MaxZipBytes) + 1
	}
	data, err := io.ReadAll(io.LimitReader(part, limit))
	if err != nil {
		return nil, uploadErr(err)
	}
	if int64(len(data)) >= limit {
		return nil, errcode.ErrSkillTooLarge
	}
	return data, nil
}

func uploadErr(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errcode.ErrSkillTooLarge
	}
	return errcode.ErrInvalidParams.WithMsg("无法读取上传的内容")
}

// ImportFile 预览暂存包里的一个文件。
func (h *SkillHandler) ImportFile(c *gin.Context) {
	var q fileQuery
	if !handler.BindQuery(c, &q) {
		return
	}
	res, err := h.svc.ImportFile(c.Request.Context(), actorID(c), c.Param("id"), q.Path)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// DiscardImport 放弃暂存。
func (h *SkillHandler) DiscardImport(c *gin.Context) {
	if err := h.svc.DiscardImport(c.Request.Context(), actorID(c), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// ConfirmImport 确认入库，返回技能。
func (h *SkillHandler) ConfirmImport(c *gin.Context) {
	res, err := h.svc.ConfirmImport(c.Request.Context(), actorID(c), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// ---- 管理端：写 ----

type enabledReq struct {
	Enabled *bool `json:"enabled" binding:"required" label:"启用状态"`
}

// SetEnabled 启用 / 停用。
func (h *SkillHandler) SetEnabled(c *gin.Context) {
	var req enabledReq
	if !handler.BindJSON(c, &req) {
		return
	}
	res, err := h.svc.SetEnabled(c.Request.Context(), actorID(c), skillName(c), *req.Enabled)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

type activeVersionReq struct {
	Version int `json:"version" binding:"required,min=1" label:"版本号"`
}

// SetActiveVersion 设置生效版本（回滚也是它）。
func (h *SkillHandler) SetActiveVersion(c *gin.Context) {
	var req activeVersionReq
	if !handler.BindJSON(c, &req) {
		return
	}
	res, err := h.svc.SetActiveVersion(c.Request.Context(), actorID(c), skillName(c), req.Version)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

type renameReq struct {
	Title string `json:"title" binding:"required,max=128" label:"显示名"`
}

// Rename 改显示名。
func (h *SkillHandler) Rename(c *gin.Context) {
	var req renameReq
	if !handler.BindJSON(c, &req) {
		return
	}
	res, err := h.svc.Rename(c.Request.Context(), actorID(c), skillName(c), req.Title)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// DeleteVersion 删除一个版本。
func (h *SkillHandler) DeleteVersion(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteVersion(c.Request.Context(), actorID(c), skillName(c), v); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Delete 删除技能（须先停用）。
func (h *SkillHandler) Delete(c *gin.Context) {
	if err := h.svc.DeleteSkill(c.Request.Context(), actorID(c), skillName(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
