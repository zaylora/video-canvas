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
}

// AssetService 负责素材的上传、生成产物转存与读取。
// 同时实现 provider.AssetStore（引擎 / 任务服务读取素材）与 provider.AssetSaver（worker 转存产物）。
type AssetService struct {
	repo  AssetRepo
	store storage.Storage
	cfg   config.Storage
	now   func() time.Time
}

var (
	_ provider.AssetStore = (*AssetService)(nil)
	_ provider.AssetSaver = (*AssetService)(nil)
)

func NewAssetService(repo AssetRepo, store storage.Storage, cfg config.Storage) *AssetService {
	return &AssetService{repo: repo, store: store, cfg: cfg, now: time.Now}
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

// View 返回当前用户的一份素材视图，URL 每次现生成（私有桶的签名地址会过期，前端需要时刷新）。
func (s *AssetService) View(ctx context.Context, userID, id uint64) (*model.AssetView, error) {
	// 1. 按 user_id 归属查询：查不到和不属于自己统一返回“不存在”，避免暴露素材是否存在
	asset, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrAssetNotFound
		}
		return nil, err
	}

	// 2. 现生成访问地址
	return s.ViewOf(ctx, asset)
}

// ViewOf 把素材行转成对前端的视图，URL 由存储实现按策略生成（公开地址或带过期时间的签名地址）。
// 调用方必须已确认素材归属当前用户。
func (s *AssetService) ViewOf(ctx context.Context, a *model.Asset) (*model.AssetView, error) {
	url, err := s.store.URL(ctx, a.StorageKey, s.cfg.SignedTTL)
	if err != nil {
		return nil, fmt.Errorf("生成素材访问地址失败: %w", err)
	}
	return assetView(a, url), nil
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

	// 2. 打开存储对象；行在但对象丢了属于数据不一致，对调用方按“素材不存在”处理，但要留日志排查
	body, err := s.store.Open(ctx, a.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			logger.Error("素材记录存在但存储对象丢失", zap.Uint64("asset_id", a.ID), zap.String("key", a.StorageKey))
			return nil, provider.ErrAssetNotFound
		}
		return nil, fmt.Errorf("打开素材失败: %w", err)
	}

	// 3. 生成 URL；失败要关掉刚打开的 Body，避免泄漏
	url, err := s.store.URL(ctx, a.StorageKey, s.cfg.SignedTTL)
	if err != nil {
		_ = body.Close()
		return nil, fmt.Errorf("生成素材访问地址失败: %w", err)
	}
	return &provider.AssetFile{Asset: a, Body: body, URL: url}, nil
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

	// 5. 生成不可猜测的对象 key：{前缀}{用户ID}/{年月}/{随机uuid}{扩展名}
	//    扩展名取自嗅探结果而不是文件名，避免客户端塞入奇怪的后缀
	key := fmt.Sprintf("%s%d/%s/%s%s", p.KeyPrefix, p.UserID, s.now().Format("200601"), newAssetUUID(), assetMimeExt[mime])

	// 6. 写入存储；失败也尝试删一次，防止对象已部分落地却没有对应记录
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, "", fmt.Errorf("回读素材临时文件失败: %w", err)
	}
	if err := s.store.Put(ctx, key, f, n, mime); err != nil {
		s.cleanup(ctx, key)
		return nil, "", fmt.Errorf("写入存储失败: %w", err)
	}

	// 7. 先生成访问 URL 再插入行：URL 只取决于 key，提前失败就不必回滚数据库行
	url, err := s.store.URL(ctx, key, s.cfg.SignedTTL)
	if err != nil {
		s.cleanup(ctx, key)
		return nil, "", fmt.Errorf("生成素材访问地址失败: %w", err)
	}

	// 8. 插入素材行；失败必须删除刚写入的对象，否则会留下永远没人引用的孤儿文件
	name := p.FileName
	if name == "" {
		name = "unnamed" + assetMimeExt[mime]
	}
	asset := &model.Asset{
		UserID:     p.UserID,
		Kind:       kind,
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
		s.cleanup(ctx, key)
		return nil, "", fmt.Errorf("保存素材记录失败: %w", err)
	}
	return asset, url, nil
}

// cleanup 尽力删除存储对象。用脱离请求取消信号的 ctx，因为常见的失败原因就是请求被取消；
// 删除失败只记日志（无法再补救，运维可按日志清理孤儿对象）。
func (s *AssetService) cleanup(ctx context.Context, key string) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), assetCleanupTimeout)
	defer cancel()
	if err := s.store.Delete(cctx, key); err != nil {
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
