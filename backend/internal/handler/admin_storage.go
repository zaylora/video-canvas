package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// StorageAdminService 是存储配置管理接口依赖的业务能力，由 service.StorageConfigService 实现。
type StorageAdminService interface {
	Presets() []storage.Preset
	List(ctx context.Context) ([]service.StorageView, error)
	Get(ctx context.Context, id uint64) (*service.StorageView, error)
	Test(ctx context.Context, in service.StorageCreateInput) (storage.ProbeResult, error)
	Create(ctx context.Context, actorID uint64, in service.StorageCreateInput) (*service.StorageView, error)
	Update(ctx context.Context, actorID, id uint64, in service.StorageUpdateInput) (*service.StorageView, error)
	ReplaceSecret(ctx context.Context, actorID, id uint64, accessKeyID, secretKey string) (*service.StorageView, error)
	Check(ctx context.Context, actorID, id uint64) (storage.ProbeResult, error)
	SetDefault(ctx context.Context, actorID, id uint64) error
	DeleteCheck(ctx context.Context, id uint64) (*service.StorageDeleteCheck, error)
	Delete(ctx context.Context, actorID, id uint64) error
}

// AdminStorageHandler 是存储配置的管理接口：列表 / 详情 / 预设（admin 可用），
// 测试 / 创建 / 更新 / 换密钥 / 重测 / 设默认 / 删除（仅 super_admin，由路由挂中间件）。
type AdminStorageHandler struct {
	svc StorageAdminService
}

// NewAdminStorageHandler 创建存储配置管理接口的 handler。
func NewAdminStorageHandler(svc StorageAdminService) *AdminStorageHandler {
	return &AdminStorageHandler{svc: svc}
}

// storageConnReq 是存储的连接与访问设置。服务商规则（桶名、地域、endpoint 推导）由 service 校验，
// 这里只拦“没传”、明显超长和枚举值不对。
type storageConnReq struct {
	AccountID     string `json:"account_id" binding:"max=64" label:"Account ID"`
	Region        string `json:"region" binding:"max=64" label:"地域"`
	Endpoint      string `json:"endpoint" binding:"max=255" label:"Endpoint"`
	Bucket        string `json:"bucket" binding:"max=128" label:"Bucket"`
	PathPrefix    string `json:"path_prefix" binding:"max=255" label:"路径前缀"`
	Addressing    string `json:"addressing" binding:"omitempty,oneof=auto virtual path" label:"寻址方式"`
	UseSSL        *bool  `json:"use_ssl"`
	PublicBaseURL string `json:"public_base_url" binding:"max=512" label:"公开访问域名"`
}

// storageTestReq 是测试一份未保存配置的参数：连接设置 + 凭证。
type storageTestReq struct {
	storageConnReq
	Provider    string `json:"provider" binding:"required,max=16" label:"服务商"`
	AccessKeyID string `json:"access_key_id" binding:"required,max=128" label:"AccessKey ID"`
	SecretKey   string `json:"secret_key" binding:"required,max=4096" label:"Secret"`
}

// storageCreateReq 是新建存储的参数。
type storageCreateReq struct {
	storageTestReq
	Name         string `json:"name" binding:"required,max=64" label:"名称"`
	SignedTTLSec int    `json:"signed_ttl_sec" binding:"min=0" label:"签名有效期"`
	DirectUpload bool   `json:"direct_upload"`
}

// storageUpdateReq 是修改存储的参数（整份表单）。服务商、AccessKey 与 Secret 不能在这里改。
type storageUpdateReq struct {
	storageConnReq
	Version      int    `json:"version" binding:"required,min=1" label:"version"`
	Name         string `json:"name" binding:"required,max=64" label:"名称"`
	SignedTTLSec int    `json:"signed_ttl_sec" binding:"min=0" label:"签名有效期"`
	DirectUpload bool   `json:"direct_upload"`
}

// storageSecretReq 是替换凭证的参数：AccessKey ID 与 Secret 必须一起换。
type storageSecretReq struct {
	AccessKeyID string `json:"access_key_id" binding:"required,max=128" label:"AccessKey ID"`
	SecretKey   string `json:"secret_key" binding:"required,max=4096" label:"Secret"`
}

// storageDefaultReq 是设置默认存储的参数。
type storageDefaultReq struct {
	ID uint64 `json:"id" binding:"required,min=1" label:"id"`
}

func (r storageTestReq) input() service.StorageCreateInput {
	return service.StorageCreateInput{
		Provider: r.Provider, AccountID: r.AccountID, Region: r.Region, Endpoint: r.Endpoint, Bucket: r.Bucket,
		PathPrefix: r.PathPrefix, Addressing: r.Addressing, UseSSL: r.UseSSL, AccessKeyID: r.AccessKeyID,
		SecretKey: r.SecretKey, PublicBaseURL: r.PublicBaseURL,
	}
}

// Presets 返回全部服务商预设（地域、直传方式、固定的寻址方式），表单据此渲染。
func (h *AdminStorageHandler) Presets(c *gin.Context) {
	response.OK(c, h.svc.Presets())
}

// List 返回所有存储。
func (h *AdminStorageHandler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get 返回存储详情。
func (h *AdminStorageHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Test 测试一份还没保存的配置。
func (h *AdminStorageHandler) Test(c *gin.Context) {
	var req storageTestReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.Test(c.Request.Context(), req.input())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Create 新建存储：保存前自动测试，密钥加密存放。
func (h *AdminStorageHandler) Create(c *gin.Context) {
	var req storageCreateReq
	if !bindJSON(c, &req) {
		return
	}
	in := req.input()
	in.Name, in.SignedTTLSec, in.DirectUpload = req.Name, req.SignedTTLSec, req.DirectUpload
	view, err := h.svc.Create(c.Request.Context(), currentUserID(c), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Update 修改存储配置（带 version 乐观锁；已有素材时定位字段被锁定）。
func (h *AdminStorageHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req storageUpdateReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.Update(c.Request.Context(), currentUserID(c), id, service.StorageUpdateInput{
		Version: req.Version, Name: req.Name, AccountID: req.AccountID, Region: req.Region, Endpoint: req.Endpoint,
		Bucket: req.Bucket, PathPrefix: req.PathPrefix, Addressing: req.Addressing, UseSSL: req.UseSSL,
		PublicBaseURL: req.PublicBaseURL, SignedTTLSec: req.SignedTTLSec, DirectUpload: req.DirectUpload,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// ReplaceSecret 替换 AccessKey ID 与 Secret：先测试新凭证，通过才替换。
func (h *AdminStorageHandler) ReplaceSecret(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req storageSecretReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.ReplaceSecret(c.Request.Context(), currentUserID(c), id, req.AccessKeyID, req.SecretKey)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Check 用已存的密钥重新测试一套存储。
func (h *AdminStorageHandler) Check(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	res, err := h.svc.Check(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// SetDefault 把一套存储设为默认：之后新上传和新生成的素材写入它，已有素材不动。
func (h *AdminStorageHandler) SetDefault(c *gin.Context) {
	var req storageDefaultReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.SetDefault(c.Request.Context(), currentUserID(c), req.ID); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// DeleteCheck 返回一套存储能不能删，以及不能删的原因。
func (h *AdminStorageHandler) DeleteCheck(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	res, err := h.svc.DeleteCheck(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Delete 删除一套存储（连同它的密钥）。桶里的文件不会被清理。
func (h *AdminStorageHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
