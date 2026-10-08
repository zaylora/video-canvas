package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

const (
	// defaultMaxUpload / defaultMaxResult 是配置未设置（为 0）时的兜底上限。
	defaultMaxUpload int64 = 200 << 20
	defaultMaxResult int64 = 2 << 30
	// assetHeadLen 用于类型嗅探的文件头长度，足够覆盖所有支持的 magic bytes。
	assetHeadLen = 32
	// assetCleanupTimeout 失败后清理已写入对象的超时；清理不能依赖可能已被取消的请求 ctx。
	assetCleanupTimeout = 10 * time.Second
	// assetMaxNameRunes 文件名最多保留的字符数（库里 file_name 列宽 255）。
	assetMaxNameRunes = 120
)

// AssetRepo 是素材服务依赖的数据访问接口，由 repository.AssetRepository 实现。
type AssetRepo interface {
	// Create 插入素材行。
	Create(ctx context.Context, a *model.Asset) error
	// GetByID 按 id 且 user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error)
	// GetByStorageKey 按对象 key 查询（不带用户条件，给 /files/<key> 路由用），不存在返回 repository.ErrNotFound。
	GetByStorageKey(ctx context.Context, key string) (*model.Asset, error)
}

// StoreRegistry 是素材服务依赖的存储解析接口，由 storage.Registry 实现。
// 写入用 Default（当时的默认存储），读取、签名、删除用 Get（按素材记录的 storage_id）。
type StoreRegistry interface {
	// Default 返回当前默认存储。
	Default(ctx context.Context) (*storage.Handle, error)
	// Get 按 id 返回存储，不存在返回 storage.ErrStorageNotFound。
	Get(ctx context.Context, id uint64) (*storage.Handle, error)
}

// AssetService 负责素材的上传、生成产物转存与读取。
// 同时实现 provider.AssetStore（引擎 / 任务服务读取素材）与 provider.AssetSaver（worker 转存产物）。
type AssetService struct {
	repo   AssetRepo
	stores StoreRegistry
	cfg    config.Storage
	now    func() time.Time

	redirects redirectCache        // /files 跳转的签名结果缓存
	intents   UploadIntentRepo     // 浏览器直传的上传意图；没有开启直传能力时为 nil
	variants  AssetVariantResolver // 缩略图 / 封面的处理服务解析；没有配置时变体一律回退
	avatars   AvatarLocator        // 头像反查（头像不在 assets 表里）；为 nil 时 /files/avatars/... 一律 404
}

// AvatarLocator 按头像 key 反查它所在的存储，由 repository.UserRepository 实现。
type AvatarLocator interface {
	// AvatarStorageID 返回正在使用该 key 作为头像的用户所记录的存储 id；没有人在用返回 repository.ErrNotFound。
	AvatarStorageID(ctx context.Context, key string) (uint64, error)
}

// SetAvatarLocator 开启 /files 对头像 key 的反查。
func (s *AssetService) SetAvatarLocator(l AvatarLocator) { s.avatars = l }

var (
	_ provider.AssetStore = (*AssetService)(nil)
	_ provider.AssetSaver = (*AssetService)(nil)
)

// NewAssetService 创建素材服务。stores 按素材记录的 storage_id 解析存储，写入用当时的默认存储。
func NewAssetService(repo AssetRepo, stores StoreRegistry, cfg config.Storage) *AssetService {
	return &AssetService{repo: repo, stores: stores, cfg: cfg, now: time.Now, redirects: redirectCache{items: map[string]redirectItem{}}}
}

// UploadInput 是用户上传素材的入参。
type UploadInput struct {
	FileName string    // 客户端给的文件名，只用于展示，会被清洗
	Size     int64     // 客户端声明的大小，未知传 -1；仅用于提前拒绝超大文件，不作为可信依据
	Body     io.Reader // 文件内容；类型以内容嗅探为准，不信任客户端的 Content-Type
}

// MaxUpload 返回单个上传文件的大小上限（供 handler 设置请求体上限）。
func (s *AssetService) MaxUpload() int64 {
	if s.cfg.MaxUpload > 0 {
		return s.cfg.MaxUpload
	}
	return defaultMaxUpload
}

// Upload 保存用户上传的素材并返回视图。
// 类型由内容嗅探决定（白名单 image/video/audio 子类型），超过 MaxUpload 返回 413。
func (s *AssetService) Upload(ctx context.Context, userID uint64, in UploadInput) (*model.AssetView, error) {
	// 1. 基本校验：必须有内容；声明的大小已超限就不必再读
	if in.Body == nil {
		return nil, errcode.ErrAssetInvalid.WithMsg("请选择要上传的文件")
	}
	max := s.MaxUpload()
	if in.Size > max {
		return nil, assetTooLarge(max)
	}

	// 2. 清洗文件名并走统一入库流程（落盘 → 嗅探 → 探测元数据 → 写存储 → 插入行）
	asset, url, err := s.ingest(ctx, assetIngest{
		UserID:    userID,
		Source:    model.AssetSourceUpload,
		KeyPrefix: "u",
		FileName:  sanitizeAssetFileName(in.FileName),
		Body:      in.Body,
		MaxBytes:  max,
	})
	if err != nil {
		var re *assetReadError
		if errors.As(err, &re) {
			// 读取请求体失败几乎都是客户端中断 / 网络问题，按参数错误返回而不是 500
			logger.Warn("读取上传内容失败", zap.Uint64("user_id", userID), zap.Error(re.err))
			return nil, errcode.ErrAssetInvalid.WithMsg("上传中断或读取文件失败，请重试")
		}
		return nil, err
	}

	// 3. 返回视图（URL 已在入库流程里生成）
	return assetView(asset, url), nil
}

// View 返回当前用户的一份素材视图，URL 是稳定地址（见 ViewOf）。
func (s *AssetService) View(ctx context.Context, userID, id uint64) (*model.AssetView, error) {
	// 1. 按 user_id 归属查询：查不到和不属于自己统一返回“不存在”，避免暴露素材是否存在
	asset, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrAssetNotFound
		}
		return nil, err
	}

	// 2. 组装视图
	return s.ViewOf(ctx, asset)
}

// ViewOf 把素材行转成对前端的视图。调用方必须已确认素材归属当前用户。
//
// URL 是稳定地址 {站点前缀}/files/<key>：它会被前端写进画布 payload 持久化，所以必须永不过期、
// 换存储也不变。签名发生在浏览器真正访问 /files 的那一刻（见 ResolveFile），而不是这里。
func (s *AssetService) ViewOf(ctx context.Context, a *model.Asset) (*model.AssetView, error) {
	return assetView(a, s.fileURL(a.StorageKey)), nil
}

// fileURL 返回素材的稳定访问地址。站点前缀沿用 storage.local.base_url（为空时是相对路径 /files）。
func (s *AssetService) fileURL(key string) string {
	return strings.TrimRight(s.cfg.Local.BaseURL, "/") + "/files/" + key
}

// signedTTL 返回一套存储的签名有效期：存储自己没配（如本地磁盘）时用全局 storage.signed_ttl。
func (s *AssetService) signedTTL(h *storage.Handle) time.Duration {
	if h.SignedTTL > 0 {
		return h.SignedTTL
	}
	return s.cfg.SignedTTL
}

// assetView 组装对前端的素材视图。
func assetView(a *model.Asset, url string) *model.AssetView {
	return &model.AssetView{
		ID:         a.ID,
		Kind:       a.Kind,
		URL:        url,
		MimeType:   a.MimeType,
		ByteSize:   a.ByteSize,
		Width:      a.Width,
		Height:     a.Height,
		DurationMs: a.DurationMs,
		FileName:   a.FileName,
	}
}

// Get 实现 provider.AssetStore：查询素材元数据，不存在或不属于该用户返回 provider.ErrAssetNotFound。
func (s *AssetService) Get(ctx context.Context, userID, assetID uint64) (*model.Asset, error) {
	// 1. 带 user_id 查询；别人的素材和不存在统一返回同一个错误
	a, err := s.repo.GetByID(ctx, userID, assetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, provider.ErrAssetNotFound
		}
		return nil, err
	}
	return a, nil
}

// Open 实现 provider.AssetStore：打开素材内容并生成可被平台访问的 URL。调用方负责关闭 Body。
func (s *AssetService) Open(ctx context.Context, userID, assetID uint64) (*provider.AssetFile, error) {
	// 1. 校验归属
	a, err := s.Get(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}

	// 2. 找到素材所在的存储（按素材自己记录的 storage_id，而不是当前默认存储）；
	//    存储配置已不存在属于数据不一致，按“素材不存在”处理并留日志
	h, err := s.stores.Get(ctx, a.StorageID)
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			logger.Error("素材所属的存储已不存在", zap.Uint64("asset_id", a.ID), zap.Uint64("storage_id", a.StorageID))
			return nil, provider.ErrAssetNotFound
		}
		return nil, fmt.Errorf("获取素材所在存储失败: %w", err)
	}

	// 3. 打开存储对象；行在但对象丢了属于数据不一致，对调用方按“素材不存在”处理，但要留日志排查
	body, err := h.Storage.Open(ctx, a.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			logger.Error("素材记录存在但存储对象丢失", zap.Uint64("asset_id", a.ID), zap.String("key", a.StorageKey))
			return nil, provider.ErrAssetNotFound
		}
		return nil, fmt.Errorf("打开素材失败: %w", err)
	}

	// 4. 生成可被上游平台访问的 URL：这里要直接给存储的签名 / 公开地址，不能给 /files 稳定地址
	//    （上游未必跟随跳转，后端地址也可能是内网地址）；失败要关掉刚打开的 Body，避免泄漏
	url, err := h.Storage.URL(ctx, a.StorageKey, s.signedTTL(h))
	if err != nil {
		_ = body.Close()
		return nil, fmt.Errorf("生成素材访问地址失败: %w", err)
	}
	return &provider.AssetFile{Asset: a, Body: body, URL: url, Remote: h.Provider != storage.ProviderLocal}, nil
}

// SaveGenerated 实现 provider.AssetSaver：把生成产物转存到自有存储并插入 assets 行，返回素材与访问 URL。
// 读取大小受 MaxBytes 限制（0 用 storage.max_result_bytes）；任何一步失败都会清理已写入的对象。
func (s *AssetService) SaveGenerated(ctx context.Context, in provider.SaveGeneratedInput) (*model.Asset, string, error) {
	// 1. 校验入参：必须有内容，种类只能是 image / video / audio 之一（或留空由内容决定）
	if in.Body == nil {
		return nil, "", errors.New("转存产物缺少内容")
	}
	if in.Kind != "" && in.Kind != "image" && in.Kind != "video" && in.Kind != "audio" {
		return nil, "", fmt.Errorf("不支持的产物种类 %q", in.Kind)
	}
	max := in.MaxBytes
	if max <= 0 {
		max = s.cfg.MaxResult
	}
	if max <= 0 {
		max = defaultMaxResult
	}

	// 2. 走统一入库流程；平台声明的 MimeType 只在嗅探不出类型时兜底（如冷门的 mp4 变体）
	taskID := in.TaskID
	name := sanitizeAssetFileName(in.FileName)
	asset, url, err := s.ingest(ctx, assetIngest{
		UserID:       in.UserID,
		Source:       model.AssetSourceGenerated,
		TaskID:       &taskID,
		KeyPrefix:    "g",
		FileName:     name,
		Body:         in.Body,
		MaxBytes:     max,
		Kind:         in.Kind,
		FallbackMime: in.MimeType,
	})
	if err != nil {
		var re *assetReadError
		if errors.As(err, &re) {
			// 下载中断属于可重试的暂时性错误，原样透传给 worker 走退避重试
			return nil, "", fmt.Errorf("读取生成产物失败: %w", re.err)
		}
		return nil, "", err
	}
	return asset, url, nil
}

// assetIngest 是统一入库流程的参数。
type assetIngest struct {
	UserID       uint64
	Source       string  // upload / generated
	TaskID       *uint64 // 生成产物对应的任务
	KeyPrefix    string  // 对象 key 首段前缀：u（用户上传）/ g（生成产物）
	FileName     string  // 已清洗的展示文件名，可为空
	Body         io.Reader
	MaxBytes     int64
	Kind         string // 期望的种类（生成产物用），为空不限制
	FallbackMime string // 嗅探失败时的兜底 mime（仅生成产物用），必须在白名单内
}

// assetReadError 标记“读取来源 Body 失败”，与本地磁盘 / 存储失败区分开。
type assetReadError struct{ err error }

func (e *assetReadError) Error() string { return "读取来源失败: " + e.err.Error() }
func (e *assetReadError) Unwrap() error { return e.err }

// ingest 是上传与转存共用的入库流程，返回已插入的素材行与它的访问 URL。
func (s *AssetService) ingest(ctx context.Context, p assetIngest) (*model.Asset, string, error) {
	// 1. 把内容限量落到临时文件：超过 MaxBytes 立刻中止。
	//    落盘而不是放内存，是因为素材可达数百 MB / GB；有了随机访问才能对 mp4 尾部的 moov 做元数据探测，
	//    同时写存储时能带上确切大小（S3 不用走分片流式上传）
	f, n, err := spoolAsset(ctx, p.Body, p.MaxBytes)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	if n == 0 {
		return nil, "", errcode.ErrAssetInvalid.WithMsg("文件内容为空")
	}

	// 2. 按 magic bytes 嗅探真实类型，忽略客户端声称的 Content-Type / 扩展名（防止伪装类型）
	head := make([]byte, assetHeadLen)
	hn, _ := f.ReadAt(head, 0)
	mime, ok := sniffAssetMime(head[:hn])
	if !ok && p.FallbackMime != "" {
		// 仅生成产物：平台声明的类型在白名单内时兜底，内容仍是自己下载的，风险可接受
		if _, allowed := assetMimeExt[p.FallbackMime]; allowed {
			mime, ok = p.FallbackMime, true
		}
	}
	if !ok {
		return nil, "", errcode.ErrAssetInvalid.WithMsg("不支持的文件类型，仅支持常见的图片、视频、音频格式")
	}

	// 3. 确定种类：以内容为准；生成产物若与期望种类冲突则拒绝。
	//    video 与 audio 允许互换，因为 mp4 / webm 容器既可能装视频也可能只装音频
	kind := assetKindOfMime(mime)
	if p.Kind != "" && p.Kind != kind {
		if (p.Kind == "image") != (kind == "image") {
			return nil, "", errcode.ErrAssetInvalid.WithMsg(fmt.Sprintf("产物类型 %s 与期望的 %s 不一致", mime, p.Kind))
		}
		kind = p.Kind
	}

	// 4. 探测元数据（宽高 / 时长）；探测失败不算错误，对应字段留 0
	meta := probeAssetMeta(mime, f, n)

	// 5. 取当前默认存储：本次写入、记录 storage_id、失败清理都用这一份，不会中途再取，
	//    所以写入过程中管理员切换默认存储，也不会出现“文件在 A、记录指向 B”
	h, err := s.stores.Default(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("获取默认存储失败: %w", err)
	}

	// 6. 生成不可猜测的对象 key：{前缀}{用户ID}/{年月}/{随机uuid}{扩展名}
	//    扩展名取自嗅探结果而不是文件名，避免客户端塞入奇怪的后缀
	key := fmt.Sprintf("%s%d/%s/%s%s", p.KeyPrefix, p.UserID, s.now().Format("200601"), newAssetUUID(), assetMimeExt[mime])

	// 7. 写入存储；失败也尝试删一次，防止对象已部分落地却没有对应记录
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, "", fmt.Errorf("回读素材临时文件失败: %w", err)
	}
	if err := h.Storage.Put(ctx, key, f, n, mime); err != nil {
		s.cleanup(ctx, h.Storage, key)
		return nil, "", fmt.Errorf("写入存储失败: %w", err)
	}

	// 8. 插入素材行；失败必须删除刚写入的对象，否则会留下永远没人引用的孤儿文件
	name := p.FileName
	if name == "" {
		name = "unnamed" + assetMimeExt[mime]
	}
	asset := &model.Asset{
		UserID:     p.UserID,
		Kind:       kind,
		StorageID:  h.ID,
		StorageKey: key,
		MimeType:   mime,
		ByteSize:   n,
		Width:      meta.Width,
		Height:     meta.Height,
		DurationMs: meta.DurationMs,
		Source:     p.Source,
		TaskID:     p.TaskID,
		FileName:   name,
	}
	if err := s.repo.Create(ctx, asset); err != nil {
		s.cleanup(ctx, h.Storage, key)
		return nil, "", fmt.Errorf("保存素材记录失败: %w", err)
	}
	url := s.fileURL(key)
	return asset, url, nil
}

// cleanup 尽力删除存储对象。用脱离请求取消信号的 ctx，因为常见的失败原因就是请求被取消；
// 删除失败只记日志（无法再补救，运维可按日志清理孤儿对象）。
func (s *AssetService) cleanup(ctx context.Context, st storage.Storage, key string) {
	cleanupObject(ctx, st, key)
}

// cleanupObject 尽力删除一个存储对象（素材清理与头像替换共用），规则同 AssetService.cleanup。
func cleanupObject(ctx context.Context, st storage.Storage, key string) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), assetCleanupTimeout)
	defer cancel()
	if err := st.Delete(cctx, key); err != nil {
		logger.Error("清理素材对象失败，可能产生孤儿文件", zap.String("key", key), zap.Error(err))
	}
}

// spoolAsset 把 r 限量复制到临时文件，返回文件与字节数（文件偏移在末尾，读取前调用方需自行 Seek 或用 ReadAt）。
// 超过 max 返回 413 错误；读取来源失败返回 *assetReadError；返回错误时临时文件已清理。
func spoolAsset(ctx context.Context, r io.Reader, max int64) (*os.File, int64, error) {
	f, err := os.CreateTemp("", "vc-asset-*")
	if err != nil {
		return nil, 0, fmt.Errorf("创建临时文件失败: %w", err)
	}
	fail := func(err error) (*os.File, int64, error) {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, 0, err
	}

	src := &assetSrcReader{ctx: ctx, r: r}
	// 多读 1 字节：读到 max+1 说明超限，而不是恰好等于上限
	n, err := io.Copy(f, io.LimitReader(src, max+1))
	if err != nil {
		if src.err != nil {
			var mbe *http.MaxBytesError
			if errors.As(src.err, &mbe) {
				return fail(assetTooLarge(max))
			}
			return fail(&assetReadError{err: src.err})
		}
		return fail(fmt.Errorf("写入临时文件失败: %w", err))
	}
	if n > max {
		return fail(assetTooLarge(max))
	}
	return f, n, nil
}

// assetSrcReader 包装来源 Reader：记录读取错误以便区分“来源失败”和“写临时文件失败”，并响应 ctx 取消。
type assetSrcReader struct {
	ctx context.Context
	r   io.Reader
	err error
}

func (a *assetSrcReader) Read(p []byte) (int, error) {
	if err := a.ctx.Err(); err != nil {
		a.err = err
		return 0, err
	}
	n, err := a.r.Read(p)
	if err != nil && err != io.EOF {
		a.err = err
	}
	return n, err
}

// assetTooLarge 构造带上限提示的 413 错误。
func assetTooLarge(max int64) *errcode.Error {
	return errcode.ErrAssetTooLarge.WithMsg(fmt.Sprintf("素材超过大小限制（最大 %d MB）", (max+(1<<20)-1)>>20))
}

// newAssetUUID 生成 UUID v4 格式的随机串，用作对象 key 的文件名，保证不可猜测。
// 用 crypto/rand 而不是 math/rand：key 是本地静态路由 / 公开桶下唯一的访问控制。
func newAssetUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("生成随机数失败: " + err.Error()) // crypto/rand 失败意味着系统熵源不可用，无法安全继续
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// sanitizeAssetFileName 清洗展示用文件名：只留最后一段（去掉目录部分）、去掉控制字符与
// 不可见格式字符（如 RTL 覆盖符）、替换 Windows 非法字符、限制长度；清洗后为空返回空串。
func sanitizeAssetFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "." || name == "/" || name == ".." {
		return ""
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r == utf8.RuneError, unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			continue
		case strings.ContainsRune(`<>:"|?*/`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.Trim(strings.TrimSpace(b.String()), ".")
	name = strings.TrimSpace(name)

	if runes := []rune(name); len(runes) > assetMaxNameRunes {
		// 截断时保留扩展名，避免丢掉类型线索
		ext := path.Ext(name)
		if extRunes := []rune(ext); len(extRunes) > 0 && len(extRunes) <= 16 {
			keep := assetMaxNameRunes - len(extRunes)
			name = string(runes[:keep]) + ext
		} else {
			name = string(runes[:assetMaxNameRunes])
		}
	}
	return name
}

// FileTarget 是 /files/<key> 的处理结果：本地存储由后端直接提供文件，对象存储跳转到签名 / 公开地址。
type FileTarget struct {
	Local       *storage.LocalStorage // 非空：由后端直接提供文件
	RedirectURL string                // 非空：302 跳转目标
	MaxAge      time.Duration         // 允许浏览器缓存这次跳转的时长
}

// ResolveFile 按对象 key 找到素材所在的存储，并决定怎么提供这个文件。
// 这条路由不鉴权（<video> / <img> 要直接引用），安全性靠 key 不可猜测，和原来的本地存储 /files 一致。
func (s *AssetService) ResolveFile(ctx context.Context, key string) (*FileTarget, error) {
	a, h, err := s.lookupFile(ctx, key)
	if err != nil {
		return nil, err
	}
	return s.targetOf(ctx, h, key, a)
}

// lookupFile 校验 key、反查素材并取得它所在的存储，ResolveFile 与 ResolveVariant 共用。
func (s *AssetService) lookupFile(ctx context.Context, key string) (*model.Asset, *storage.Handle, error) {
	// 1. 非法 key 不可能对应任何素材，直接按不存在处理，不必查库
	if err := storage.ValidateKey(key); err != nil {
		return nil, nil, errcode.ErrAssetNotFound
	}

	// 2. 反查素材：拿到它所在的存储。头像不进 assets 表，按 avatars/ 前缀改从用户表反查
	var (
		a   *model.Asset
		err error
	)
	if strings.HasPrefix(key, AvatarKeyPrefix) {
		a, err = s.lookupAvatar(ctx, key)
	} else {
		a, err = s.repo.GetByStorageKey(ctx, key)
	}
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, errcode.ErrAssetNotFound
		}
		return nil, nil, err
	}
	h, err := s.stores.Get(ctx, a.StorageID)
	if err != nil {
		// 内部原因（解密失败、配置被删等）只写日志，不透传给浏览器
		logger.Error("解析素材所在存储失败", zap.Uint64("asset_id", a.ID), zap.Uint64("storage_id", a.StorageID), zap.Error(err))
		return nil, nil, errcode.ErrStorageUnavailable
	}
	return a, h, nil
}

// lookupAvatar 把头像 key 包装成一条只含存储定位信息的“素材”，让 /files 的后续流程（本地直出 / 签名跳转 / 缩略图）原样复用。
// 只有当前正在使用的头像能被访问：被替换或移除的旧 key 即使文件还没删掉，也返回不存在。
func (s *AssetService) lookupAvatar(ctx context.Context, key string) (*model.Asset, error) {
	if s.avatars == nil {
		return nil, repository.ErrNotFound
	}
	storageID, err := s.avatars.AvatarStorageID(ctx, key)
	if err != nil {
		return nil, err
	}
	return &model.Asset{StorageID: storageID, StorageKey: key, Kind: "image"}, nil
}

// targetOf 决定原文件怎么提供：本地磁盘由后端直接提供，对象存储跳转到签名（或公开）地址。
func (s *AssetService) targetOf(ctx context.Context, h *storage.Handle, key string, a *model.Asset) (*FileTarget, error) {
	// 1. 本地磁盘：后端直接提供（支持 Range，视频可拖动）
	if ls, ok := h.Storage.(*storage.LocalStorage); ok {
		return &FileTarget{Local: ls}, nil
	}

	// 2. 对象存储：跳转到签名（或公开）地址。同一个文件在 ttl/2 内复用同一个地址，
	//    这样浏览器对图片、视频的缓存才能命中；浏览器最多缓存 ttl/4，保证拿到的地址一定还没过期
	ttl := s.signedTTL(h)
	if ttl <= 0 {
		ttl = time.Hour
	}
	cacheKey := fmt.Sprintf("%d/%s", h.ID, key)
	now := s.now()
	if url, ok := s.redirects.get(cacheKey, now); ok {
		return &FileTarget{RedirectURL: url, MaxAge: ttl / 4}, nil
	}
	url, err := h.Storage.URL(ctx, key, ttl)
	if err != nil {
		logger.Error("生成素材跳转地址失败", zap.Uint64("storage_id", h.ID), zap.Error(err))
		return nil, errcode.ErrStorageUnavailable
	}
	s.redirects.put(cacheKey, url, now.Add(ttl/2), now)
	return &FileTarget{RedirectURL: url, MaxAge: ttl / 4}, nil
}

// redirectCacheMax 是跳转地址缓存的条目上限，防止被大量不同 key 撑爆内存。
const redirectCacheMax = 10000

type redirectItem struct {
	url string
	exp time.Time
}

// redirectCache 缓存 /files 跳转用的签名地址。
type redirectCache struct {
	mu    sync.Mutex
	items map[string]redirectItem
}

func (c *redirectCache) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok || !now.Before(it.exp) {
		return "", false
	}
	return it.url, true
}

// put 写入缓存；超过上限时先清掉已过期的项，仍然超限就整体清空（缓存丢了只是多签几次名）。
func (c *redirectCache) put(key, url string, exp, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= redirectCacheMax {
		for k, it := range c.items {
			if !now.Before(it.exp) {
				delete(c.items, k)
			}
		}
		if len(c.items) >= redirectCacheMax {
			c.items = map[string]redirectItem{}
		}
	}
	c.items[key] = redirectItem{url: url, exp: exp}
}
