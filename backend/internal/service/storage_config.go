package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// StorageConfigRepo 是存储配置服务的数据访问接口，由 repository.StorageConfigRepository 实现。
type StorageConfigRepo interface {
	// Create 新建存储；名称重复返回 repository.ErrDuplicate。
	Create(ctx context.Context, s *model.StorageConfig) error
	// GetByID 按 id 查询，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.StorageConfig, error)
	// GetDefault 返回默认存储，没有返回 repository.ErrNotFound。
	GetDefault(ctx context.Context) (*model.StorageConfig, error)
	// List 返回全部存储，按 id 升序。
	List(ctx context.Context) ([]model.StorageConfig, error)
	// Update 带乐观锁更新并把 version +1：不存在返回 ErrNotFound，版本不一致返回 ErrRevisionConflict，名称重复返回 ErrDuplicate。
	Update(ctx context.Context, id uint64, version int, fields map[string]any) error
	// RecordCheck 记录最近一次测试连接的结果，不改 version。
	RecordCheck(ctx context.Context, id uint64, ok bool, errMsg string, at time.Time) error
	// SetDefault 把一套存储设为默认，不存在返回 ErrNotFound。
	SetDefault(ctx context.Context, id uint64) error
	// AssetCounts 返回每套存储被多少素材引用。
	AssetCounts(ctx context.Context) (map[uint64]int64, error)
	// CountRefs 返回素材数与未过期的未完成上传意图数，存储不存在返回 ErrNotFound。
	CountRefs(ctx context.Context, id uint64, now time.Time) (assets, intents int64, err error)
	// Delete 事务内重新统计引用后删除存储与它的密钥：仍被引用返回 ErrInUse，不存在返回 ErrNotFound。
	Delete(ctx context.Context, id uint64, now time.Time) error
	// BackfillAssets 把还没记录存储的旧素材（storage_id = 0）全部指向 id，返回更新的行数。
	BackfillAssets(ctx context.Context, id uint64) (int64, error)
}

// StorageSecrets 是存储密钥的存取依赖，由 AIConfigService 实现（凭证加解密都在那里）。
type StorageSecrets interface {
	// SetSecret 设置（覆盖）凭证，只写。
	SetSecret(ctx context.Context, name, value string, adminID uint64) error
	// SecretIsSet 判断凭证是否已设置，不解密。
	SecretIsSet(ctx context.Context, name string) (bool, error)
	// Get 解密并返回凭证明文，只用来创建存储客户端，绝不进响应与日志。
	Get(ctx context.Context, name string) (string, error)
	// ForgetSecret 清掉本实例对该凭证的明文缓存。
	ForgetSecret(name string)
}

// StorageProber 对一份配置创建存储客户端并执行测试连接。
// 配置不合法、没法创建客户端时返回 error（可用 errors.Is(err, storage.ErrInvalidSpec) 判断）。
type StorageProber interface {
	Probe(ctx context.Context, spec storage.Spec) (storage.ProbeResult, error)
}

// StorageInvalidator 在配置变更后让存储客户端缓存失效，由 storage.Registry 实现。id 为 0 表示全部。
type StorageInvalidator interface {
	Invalidate(id uint64)
}

// defaultStorageProber 用真实的存储客户端做测试连接。
type defaultStorageProber struct{}

func (defaultStorageProber) Probe(ctx context.Context, spec storage.Spec) (storage.ProbeResult, error) {
	st, err := storage.NewFromSpec(spec)
	if err != nil {
		return storage.ProbeResult{}, err
	}
	return storage.Probe(ctx, st, storage.ProbeOptions{}), nil
}

const (
	storageNameMaxLen   = 64
	storageDefaultTTL   = 3600   // 签名有效期默认 1 小时
	storageMinTTL       = 60     // 至少 1 分钟
	storageMaxTTL       = 604800 // 最长 7 天（S3 V4 预签名的上限）
	storageProbeTimeout = time.Minute
)

// StorageConfigService 管理素材存储配置：新建 / 修改 / 换密钥 / 测试连接 / 设默认 / 删除，
// 同时实现 storage.Source，给 storage.Registry 提供解密后的运行时配置。
// 写操作只有 super_admin 能调（由路由的中间件保证）。
type StorageConfigService struct {
	repo    StorageConfigRepo
	secrets StorageSecrets
	audit   AIAuditWriter
	prober  StorageProber
	inv     StorageInvalidator
	local   config.LocalStorage // 内置本地存储的目录配置，来自 YAML
	now     func() time.Time
}

var _ storage.Source = (*StorageConfigService)(nil)

// NewStorageConfigService 创建存储配置服务。prober 为 nil 时用真实的存储客户端。
func NewStorageConfigService(repo StorageConfigRepo, secrets StorageSecrets, audit AIAuditWriter, prober StorageProber, cfg config.Storage) *StorageConfigService {
	if prober == nil {
		prober = defaultStorageProber{}
	}
	return &StorageConfigService{repo: repo, secrets: secrets, audit: audit, prober: prober, local: cfg.Local, now: time.Now}
}

// SetInvalidator 注入缓存失效器。Registry 依赖本服务读配置、本服务又要在变更后让 Registry 失效，
// 所以组装完成后再注入。
func (s *StorageConfigService) SetInvalidator(inv StorageInvalidator) { s.inv = inv }

func (s *StorageConfigService) invalidate(id uint64) {
	if s.inv != nil {
		s.inv.Invalidate(id)
	}
}

// ---- 视图与入参 ----

// StorageCheck 是最近一次测试连接的结果。
type StorageCheck struct {
	OK    bool      `json:"ok"`
	At    time.Time `json:"at"`
	Error string    `json:"error"`
}

// StorageView 是管理端的存储视图。密钥只写不读：只告诉调用方有没有设置（secret_set），AccessKey ID 脱敏展示。
type StorageView struct {
	ID            uint64        `json:"id"`
	Name          string        `json:"name"`
	Provider      string        `json:"provider"`
	Builtin       bool          `json:"builtin"`
	LocalDir      string        `json:"local_dir,omitempty"` // 仅内置本地存储
	AccountID     string        `json:"account_id"`
	Endpoint      string        `json:"endpoint"`
	Region        string        `json:"region"`
	Bucket        string        `json:"bucket"`
	PathPrefix    string        `json:"path_prefix"`
	Addressing    string        `json:"addressing"`
	UseSSL        bool          `json:"use_ssl"`
	AccessKeyID   string        `json:"access_key_id"` // 已脱敏
	SecretSet     bool          `json:"secret_set"`
	PublicBaseURL string        `json:"public_base_url"`
	Access        string        `json:"access"` // private（签名）/ public（公开域名或 CDN）
	SignedTTLSec  int           `json:"signed_ttl_sec"`
	DirectUpload  bool          `json:"direct_upload"`
	DirectMethod  string        `json:"direct_method"` // 跟着服务商预设走；本地存储为空
	IsDefault     bool          `json:"is_default"`
	AssetCount    int64         `json:"asset_count"`
	Locked        bool          `json:"locked"` // 已有素材引用：定位字段不可修改
	Check         *StorageCheck `json:"check"`  // 没测过为 nil
	Version       int           `json:"version"`
	UpdatedAt     time.Time     `json:"updated_at"`
	CreatedAt     time.Time     `json:"created_at"`
}

// StorageCreateInput 是新建存储（以及测试一份未保存的配置）的参数。
type StorageCreateInput struct {
	Name          string
	Provider      string
	AccountID     string // 仅 R2
	Region        string
	Endpoint      string // 仅 S3 自定义
	Bucket        string
	PathPrefix    string
	Addressing    string // 仅 S3 可选
	UseSSL        *bool  // 仅 S3 自定义 endpoint 有意义，为空按 true
	AccessKeyID   string
	SecretKey     string
	PublicBaseURL string
	SignedTTLSec  int // 0 用默认 1 小时
	DirectUpload  bool
}

// StorageUpdateInput 是修改存储的参数（整份表单提交，不是部分更新）。密钥与 AccessKey ID 不在这里，走 ReplaceSecret。
type StorageUpdateInput struct {
	Version       int // 读到的版本，乐观锁
	Name          string
	AccountID     string
	Region        string
	Endpoint      string
	Bucket        string
	PathPrefix    string
	Addressing    string
	UseSSL        *bool
	PublicBaseURL string
	SignedTTLSec  int
	DirectUpload  bool
}

// StorageDeleteCheck 回答“这套存储能不能删”，以及不能删的原因。
type StorageDeleteCheck struct {
	Deletable      bool   `json:"deletable"`
	Reason         string `json:"reason"`
	AssetCount     int64  `json:"asset_count"`
	PendingUploads int64  `json:"pending_uploads"`
}

// maskAccessKey 脱敏 AccessKey ID：只留首 4 位和末 3 位，太短的全部遮掉。
func maskAccessKey(ak string) string {
	if len(ak) <= 8 {
		return "••••"
	}
	return ak[:4] + "••••" + ak[len(ak)-3:]
}

// Presets 返回全部服务商预设（地域列表、直传方式、固定的寻址方式），给后台表单用。
func (s *StorageConfigService) Presets() []storage.Preset { return storage.Presets() }

func (s *StorageConfigService) view(row *model.StorageConfig, assets int64, secretSet bool) *StorageView {
	v := &StorageView{
		ID: row.ID, Name: row.Name, Provider: row.Provider, Builtin: row.Builtin, AccountID: row.AccountID,
		Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket, PathPrefix: row.PathPrefix, Addressing: row.Addressing,
		UseSSL: row.UseSSL, SecretSet: secretSet, PublicBaseURL: row.PublicBaseURL, Access: "private", SignedTTLSec: row.SignedTTLSec,
		DirectUpload: row.DirectUpload, IsDefault: row.IsDefault, AssetCount: assets, Locked: assets > 0,
		Version: row.Version, UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt,
	}
	if row.AccessKeyID != "" {
		v.AccessKeyID = maskAccessKey(row.AccessKeyID)
	}
	if row.PublicBaseURL != "" {
		v.Access = "public"
	}
	if row.Builtin {
		v.LocalDir = s.local.Dir
	}
	if p, ok := storage.PresetOf(row.Provider); ok {
		v.DirectMethod = p.DirectMethod
	}
	if row.LastCheckOK != nil {
		c := &StorageCheck{OK: *row.LastCheckOK, Error: row.LastCheckError}
		if row.LastCheckAt != nil {
			c.At = *row.LastCheckAt
		}
		v.Check = c
	}
	return v
}

// List 返回全部存储（内置本地磁盘在最前），带素材数、是否被锁定、最近一次测试结果。
func (s *StorageConfigService) List(ctx context.Context) ([]StorageView, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.AssetCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StorageView, 0, len(rows))
	for i := range rows {
		set, err := s.secretSet(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *s.view(&rows[i], counts[rows[i].ID], set))
	}
	return out, nil
}

// Get 返回一套存储的详情，不存在返回 404。
func (s *StorageConfigService) Get(ctx context.Context, id uint64) (*StorageView, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, storageNotFound(err)
	}
	return s.viewOf(ctx, row)
}

func (s *StorageConfigService) viewOf(ctx context.Context, row *model.StorageConfig) (*StorageView, error) {
	assets, intents, err := s.repo.CountRefs(ctx, row.ID, s.now())
	if err != nil {
		return nil, storageNotFound(err)
	}
	set, err := s.secretSet(ctx, row)
	if err != nil {
		return nil, err
	}
	v := s.view(row, assets, set)
	v.Locked = assets+intents > 0
	return v, nil
}

// secretSet 判断存储是否已设置密钥；内置本地存储没有密钥。
func (s *StorageConfigService) secretSet(ctx context.Context, row *model.StorageConfig) (bool, error) {
	if row.Provider == storage.ProviderLocal {
		return false, nil
	}
	return s.secrets.SecretIsSet(ctx, model.StorageSecretName(row.ID))
}

func storageNotFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrStorageNotFound
	}
	return err
}

// ---- 校验与规范化 ----

// validateCommon 校验名称与签名有效期，返回规范化后的值。
func validateStorageCommon(name string, ttl int) (string, int, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > storageNameMaxLen {
		return "", 0, errcode.ErrStorageInvalid.WithMsg(fmt.Sprintf("名称不能为空，且不超过 %d 个字符", storageNameMaxLen))
	}
	if ttl == 0 {
		ttl = storageDefaultTTL
	}
	if ttl < storageMinTTL || ttl > storageMaxTTL {
		return "", 0, errcode.ErrStorageInvalid.WithMsg("签名有效期需要在 1 分钟到 7 天之间")
	}
	return name, ttl, nil
}

func invalidSpec(err error) error {
	return errcode.ErrStorageInvalid.WithMsg(strings.TrimPrefix(err.Error(), storage.ErrInvalidSpec.Error()+"："))
}

// specOf 把存储行和密钥组装成客户端配置。
func specOf(row *model.StorageConfig, secret string) storage.Spec {
	return storage.Spec{
		Provider: row.Provider, Endpoint: row.Endpoint, Region: row.Region, AccountID: row.AccountID, Bucket: row.Bucket,
		PathPrefix: row.PathPrefix, Addressing: row.Addressing, UseSSL: row.UseSSL,
		AccessKey: row.AccessKeyID, SecretKey: secret, PublicBaseURL: row.PublicBaseURL,
	}
}

func useSSLOrDefault(p *bool) bool { return p == nil || *p }

// failureText 把探针结果里第一个失败步骤写成一句话，存进 last_check_error / 错误提示。
func failureText(res storage.ProbeResult) string {
	for _, st := range res.Steps {
		if !st.OK && st.Issue != nil {
			return fmt.Sprintf("第 %d 步「%s」失败：%s", st.Index, st.Name, st.Issue.Title)
		}
	}
	return ""
}

// runProbe 带超时地执行测试连接。创建不了客户端（配置不合法）返回业务错误；探针本身的失败体现在结果里。
func (s *StorageConfigService) runProbe(ctx context.Context, spec storage.Spec) (storage.ProbeResult, error) {
	pctx, cancel := context.WithTimeout(ctx, storageProbeTimeout)
	defer cancel()
	res, err := s.prober.Probe(pctx, spec)
	if err != nil {
		if errors.Is(err, storage.ErrInvalidSpec) {
			return res, invalidSpec(err)
		}
		return res, errcode.ErrStorageInvalid.WithMsg(err.Error())
	}
	return res, nil
}

// ---- 新建 / 测试 ----

// buildCreateSpec 校验并规范化新建参数，返回带密钥的客户端配置。
func buildCreateSpec(in StorageCreateInput) (storage.Spec, error) {
	in.AccessKeyID, in.SecretKey = strings.TrimSpace(in.AccessKeyID), strings.TrimSpace(in.SecretKey)
	if in.AccessKeyID == "" || in.SecretKey == "" {
		return storage.Spec{}, errcode.ErrStorageInvalid.WithMsg("AccessKey ID 与 Secret 不能为空")
	}
	spec, err := (storage.Spec{
		Provider: in.Provider, Endpoint: in.Endpoint, Region: in.Region, AccountID: in.AccountID, Bucket: in.Bucket,
		PathPrefix: in.PathPrefix, Addressing: in.Addressing, UseSSL: useSSLOrDefault(in.UseSSL),
		AccessKey: in.AccessKeyID, SecretKey: in.SecretKey, PublicBaseURL: in.PublicBaseURL,
	}).Normalize()
	if err != nil {
		return storage.Spec{}, invalidSpec(err)
	}
	return spec, nil
}

// Test 测试一份还没保存的配置：不落库、不写密钥，给表单里的“测试连接”按钮用。
func (s *StorageConfigService) Test(ctx context.Context, in StorageCreateInput) (storage.ProbeResult, error) {
	spec, err := buildCreateSpec(in)
	if err != nil {
		return storage.ProbeResult{}, err
	}
	return s.runProbe(ctx, spec)
}

// Create 新建一套存储：先用提交的密钥测试连接，再落库，最后把密钥加密存进 ai_secrets。
// 测试不通过也会保存（方便先存草稿、网络调通后再测），但会记录失败原因，并且不能被设为默认。
func (s *StorageConfigService) Create(ctx context.Context, actorID uint64, in StorageCreateInput) (*StorageView, error) {
	// 1. 校验参数并规范化（OSS / COS / R2 的 endpoint 按预设推导）
	name, ttl, err := validateStorageCommon(in.Name, in.SignedTTLSec)
	if err != nil {
		return nil, err
	}
	spec, err := buildCreateSpec(in)
	if err != nil {
		return nil, err
	}

	// 2. 保存前先测试一次：此时密钥还没入库，直接用提交的明文
	res, err := s.runProbe(ctx, spec)
	if err != nil {
		return nil, err
	}

	// 3. 落库。名称重复在这里发现，此时还没写密钥，不需要回滚
	now := s.now()
	row := &model.StorageConfig{
		Name: name, Provider: spec.Provider, AccountID: spec.AccountID, Endpoint: spec.Endpoint, Region: spec.Region,
		Bucket: spec.Bucket, PathPrefix: spec.PathPrefix, Addressing: spec.Addressing, UseSSL: spec.UseSSL,
		AccessKeyID: spec.AccessKey, PublicBaseURL: spec.PublicBaseURL, SignedTTLSec: ttl, DirectUpload: in.DirectUpload,
		Version: 1, CreatedBy: actorID, UpdatedBy: actorID,
	}
	if spec.Provider != storage.ProviderR2 {
		row.AccountID = ""
	}
	if err := s.repo.Create(ctx, row); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errcode.ErrStorageNameDup
		}
		return nil, err
	}

	// 4. 写密钥。失败（如主密钥未配置）要回滚刚建的配置，否则会留下一套永远用不了的存储
	if err := s.secrets.SetSecret(ctx, model.StorageSecretName(row.ID), spec.SecretKey, actorID); err != nil {
		if derr := s.repo.Delete(ctx, row.ID, now); derr != nil {
			logger.Error("回滚新建的存储失败，请手动清理", zap.Uint64("storage_id", row.ID), zap.Error(derr))
		}
		return nil, err
	}

	// 5. 记录测试结果与审计（审计只记配置，不含密钥）
	s.recordResult(ctx, row.ID, res, now)
	aiAudit(ctx, s.audit, actorID, model.AuditStorageCreate, model.AuditTargetStorage, strconv.FormatUint(row.ID, 10),
		map[string]any{"name": name, "provider": spec.Provider, "bucket": spec.Bucket, "check_ok": res.OK})
	return s.Get(ctx, row.ID)
}

// recordResult 把测试结果写回存储行。写失败只记日志：结果是辅助信息，不该让已经成功的操作失败。
func (s *StorageConfigService) recordResult(ctx context.Context, id uint64, res storage.ProbeResult, at time.Time) {
	if err := s.repo.RecordCheck(ctx, id, res.OK, failureText(res), at); err != nil {
		logger.Warn("记录存储测试结果失败", zap.Uint64("storage_id", id), zap.Error(err))
	}
}
