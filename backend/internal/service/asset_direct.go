package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

const (
	// uploadIntentTTL 是一次直传申请的有效期：超过它还没登记，对象会被清理任务删除。
	uploadIntentTTL = 15 * time.Minute
	// directHeadLen 是登记时读取的文件头长度：用来嗅探类型和探测宽高 / 时长，不整份下载。
	// mp4 的 moov 在文件尾时读不到时长，对应字段留 0，和现有探测逻辑一致。
	directHeadLen = 1 << 20
)

// UploadIntentRepo 是浏览器直传的上传意图的数据访问接口，由 repository.UploadIntentRepository 实现。
type UploadIntentRepo interface {
	// Create 记录一条上传意图。
	Create(ctx context.Context, in *model.AssetUploadIntent) error
	// GetByID 按 id 且 user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.AssetUploadIntent, error)
	// Complete 标记完成，已完成或不存在返回 repository.ErrNotFound。
	Complete(ctx context.Context, id uint64, at time.Time) error
	// ListExpired 返回已过期且未完成的意图。
	ListExpired(ctx context.Context, now time.Time, limit int) ([]model.AssetUploadIntent, error)
	// Delete 删除一条意图，不存在不算错误。
	Delete(ctx context.Context, id uint64) error
}

// SetUploadIntents 注入上传意图仓储，开启浏览器直传能力。没有注入时所有上传都回退到后端中转。
func (s *AssetService) SetUploadIntents(r UploadIntentRepo) { s.intents = r }

// UploadIntentInput 是申请直传的参数。
type UploadIntentInput struct {
	FileName string // 客户端文件名，只用于展示，会被清洗
	Size     int64  // 文件大小，必须提供：直传时它会被签进地址 / 用来复核
	MimeType string // 客户端声明的类型，必须在白名单内；登记时会再按内容嗅探
}

// UploadIntentView 是申请直传的结果。Mode 为 proxy 表示当前存储不支持（或没开）直传，客户端应走 POST /assets。
type UploadIntentView struct {
	Mode      string            `json:"mode"` // proxy / direct
	IntentID  uint64            `json:"intent_id,omitempty"`
	Method    string            `json:"method,omitempty"`  // post / put
	URL       string            `json:"url,omitempty"`     // 上传地址
	Fields    map[string]string `json:"fields,omitempty"`  // post：必须原样放进表单的字段
	Headers   map[string]string `json:"headers,omitempty"` // put：必须带上的请求头
	ExpiresAt time.Time         `json:"expires_at,omitempty"`
}

// CreateUploadIntent 申请一次浏览器直传：校验参数、记录意图、签发上传凭证。
//
// 写到哪套存储在这一刻就定下来，记在意图里；之后 CompleteUpload 一律按它找存储，不看当时的默认存储，
// 所以上传中途管理员切换默认存储，也不会出现“文件在 A、记录指向 B”。
func (s *AssetService) CreateUploadIntent(ctx context.Context, userID uint64, in UploadIntentInput) (*UploadIntentView, error) {
	// 1. 校验入参：直传必须知道大小；类型必须在白名单（登记时还会按内容再嗅探一次）
	limit := s.MaxUpload()
	if in.Size <= 0 {
		return nil, errcode.ErrAssetInvalid.WithMsg("请提供文件大小")
	}
	if in.Size > limit {
		return nil, assetTooLarge(limit)
	}
	ext, ok := assetMimeExt[in.MimeType]
	if !ok {
		return nil, errcode.ErrAssetInvalid.WithMsg("不支持的文件类型，仅支持常见的图片、视频、音频格式")
	}

	// 2. 取默认存储。管理员没开直传、或存储不支持（如本地磁盘）时回退到后端中转，这不是错误
	h, err := s.stores.Default(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取默认存储失败: %w", err)
	}
	up, ok := h.Storage.(storage.DirectUploader)
	if s.intents == nil || !h.DirectUpload || !ok {
		return &UploadIntentView{Mode: "proxy"}, nil
	}

	// 3. 先记录意图再签发：这样任何签发出去的凭证都一定有记录可查，上传了却没人登记的对象才能被清理任务找到
	key := fmt.Sprintf("u%d/%s/%s%s", userID, s.now().Format("200601"), newAssetUUID(), ext)
	intent := &model.AssetUploadIntent{
		UserID: userID, StorageID: h.ID, StorageKey: key, FileName: sanitizeAssetFileName(in.FileName),
		MimeType: in.MimeType, Kind: assetKindOfMime(in.MimeType), DeclaredSize: in.Size, ExpiresAt: s.now().Add(uploadIntentTTL),
	}
	if err := s.intents.Create(ctx, intent); err != nil {
		return nil, fmt.Errorf("记录上传申请失败: %w", err)
	}

	// 4. 签发凭证；失败要撤销刚记录的意图，不留没有凭证的孤儿记录
	du, err := up.DirectUpload(ctx, storage.DirectUploadRequest{
		Key: key, ContentType: in.MimeType, Size: in.Size, MaxSize: limit, TTL: uploadIntentTTL,
	})
	if err != nil {
		if derr := s.intents.Delete(ctx, intent.ID); derr != nil {
			logger.Warn("撤销上传申请失败", zap.Uint64("intent_id", intent.ID), zap.Error(derr))
		}
		if errors.Is(err, storage.ErrTooLarge) {
			return nil, assetTooLarge(limit)
		}
		return nil, fmt.Errorf("签发上传凭证失败: %w", err)
	}
	return &UploadIntentView{
		Mode: "direct", IntentID: intent.ID, Method: du.Method, URL: du.URL, Fields: du.Fields, Headers: du.Headers, ExpiresAt: intent.ExpiresAt,
	}, nil
}

// CompleteUpload 登记一次直传：浏览器已经把文件传到桶里，这里复核大小、按内容嗅探类型、探测元数据，然后插入素材行。
// 直传绕过了后端，所以这里是唯一的把关点：不合法的对象会被删除，不会留在桶里。
func (s *AssetService) CompleteUpload(ctx context.Context, userID, intentID uint64) (*model.AssetView, error) {
	// 1. 按用户读取意图；别人的和不存在统一返回“不存在”
	if s.intents == nil {
		return nil, errcode.ErrUploadDirectDisabled
	}
	in, err := s.intents.GetByID(ctx, userID, intentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrUploadIntentNotFound
		}
		return nil, err
	}

	// 2. 已经登记过：幂等返回同一个素材（浏览器重试、网络抖动都会重复提交）
	if in.CompletedAt != nil {
		return s.viewByKey(ctx, in.StorageKey)
	}

	// 3. 找到申请时的存储（不是当前默认存储），并确认它支持复核
	h, stat, ranger, err := s.directStore(ctx, in)
	if err != nil {
		return nil, err
	}

	// 4. 超过有效期：对象即使传上来了也不收，删除并按不存在处理
	if !s.now().Before(in.ExpiresAt) {
		s.discardUpload(ctx, h, in)
		return nil, errcode.ErrUploadIntentNotFound
	}

	// 5. 复核对象：还没传完 / 实际大小与申请不一致
	info, err := stat.Stat(ctx, in.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, errcode.ErrAssetInvalid.WithMsg("文件尚未上传完成，请稍后重试")
		}
		return nil, fmt.Errorf("读取上传对象信息失败: %w", err)
	}
	if info.Size != in.DeclaredSize {
		s.discardUpload(ctx, h, in)
		return nil, errcode.ErrUploadSizeMismatch
	}

	// 6. 只读文件头：按内容嗅探真实类型（不信任客户端声明），探测宽高 / 时长
	head, err := readHead(ctx, ranger, in.StorageKey, min(info.Size, directHeadLen))
	if err != nil {
		return nil, err
	}
	mime, ok := sniffAssetMime(head[:min(len(head), assetHeadLen)])
	if !ok {
		s.discardUpload(ctx, h, in)
		return nil, errcode.ErrAssetInvalid.WithMsg("不支持的文件类型，仅支持常见的图片、视频、音频格式")
	}
	meta := probeAssetMeta(mime, bytes.NewReader(head), info.Size)

	// 7. 插入素材行：失败时保留对象和意图，让客户端可以重试；
	//    并发的重复登记会撞上 storage_key 唯一约束，此时直接返回已登记的那一份
	name := in.FileName
	if name == "" {
		name = "unnamed" + assetMimeExt[mime]
	}
	asset := &model.Asset{
		UserID: userID, Kind: assetKindOfMime(mime), StorageID: in.StorageID, StorageKey: in.StorageKey, MimeType: mime,
		ByteSize: info.Size, Width: meta.Width, Height: meta.Height, DurationMs: meta.DurationMs,
		Source: model.AssetSourceUpload, FileName: name,
	}
	if err := s.repo.Create(ctx, asset); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return s.viewByKey(ctx, in.StorageKey)
		}
		return nil, fmt.Errorf("保存素材记录失败: %w", err)
	}

	// 8. 标记意图完成。失败不影响结果（素材已登记，重复提交会走第 2 步的幂等分支）
	if err := s.intents.Complete(ctx, in.ID, s.now()); err != nil && !errors.Is(err, repository.ErrNotFound) {
		logger.Warn("标记上传意图完成失败", zap.Uint64("intent_id", in.ID), zap.Error(err))
	}
	return assetView(asset, s.fileURL(asset.StorageKey)), nil
}

// directStore 找到意图申请时的存储，并确认它支持登记所需的复核能力（Stat 与按范围读取）。
func (s *AssetService) directStore(ctx context.Context, in *model.AssetUploadIntent) (*storage.Handle, storage.Statter, storage.RangeOpener, error) {
	h, err := s.stores.Get(ctx, in.StorageID)
	if err != nil {
		logger.Error("上传意图所属的存储不可用", zap.Uint64("intent_id", in.ID), zap.Uint64("storage_id", in.StorageID), zap.Error(err))
		return nil, nil, nil, errcode.ErrStorageUnavailable
	}
	stat, canStat := h.Storage.(storage.Statter)
	ranger, canRange := h.Storage.(storage.RangeOpener)
	if !canStat || !canRange {
		return nil, nil, nil, errcode.ErrUploadDirectDisabled
	}
	return h, stat, ranger, nil
}

// viewByKey 按对象 key 返回已登记素材的视图。
func (s *AssetService) viewByKey(ctx context.Context, key string) (*model.AssetView, error) {
	a, err := s.repo.GetByStorageKey(ctx, key)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrUploadIntentNotFound
		}
		return nil, err
	}
	return assetView(a, s.fileURL(a.StorageKey)), nil
}

// readHead 读取对象的前 n 个字节。
func readHead(ctx context.Context, r storage.RangeOpener, key string, n int64) ([]byte, error) {
	if n <= 0 {
		return nil, errcode.ErrAssetInvalid.WithMsg("文件内容为空")
	}
	rc, err := r.OpenRange(ctx, key, 0, n)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, errcode.ErrAssetInvalid.WithMsg("文件尚未上传完成，请稍后重试")
		}
		return nil, fmt.Errorf("读取上传对象失败: %w", err)
	}
	defer rc.Close()
	head, err := io.ReadAll(io.LimitReader(rc, n))
	if err != nil {
		return nil, fmt.Errorf("读取上传对象失败: %w", err)
	}
	return head, nil
}

// discardUpload 删除一次不合法 / 过期的直传：对象和意图都删，失败只记日志（过期清理任务会兜底）。
func (s *AssetService) discardUpload(ctx context.Context, h *storage.Handle, in *model.AssetUploadIntent) {
	s.cleanup(ctx, h.Storage, in.StorageKey)
	if err := s.intents.Delete(context.WithoutCancel(ctx), in.ID); err != nil {
		logger.Warn("删除上传意图失败", zap.Uint64("intent_id", in.ID), zap.Error(err))
	}
}

// CleanupUploads 清理过期未登记的直传：浏览器可能已经把对象传到桶里却没有登记（关了页面、网络断了），
// 这些对象没有任何素材引用，留着就是孤儿文件。按意图里记录的存储逐个删除对象与意图，返回清理条数。
// 单条失败不影响其余，留到下一轮重试。
func (s *AssetService) CleanupUploads(ctx context.Context, limit int) (int, error) {
	if s.intents == nil {
		return 0, nil
	}
	rows, err := s.intents.ListExpired(ctx, s.now(), limit)
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for i := range rows {
		in := &rows[i]
		h, err := s.stores.Get(ctx, in.StorageID)
		if err != nil {
			logger.Warn("清理过期上传：存储不可用，留到下一轮", zap.Uint64("intent_id", in.ID), zap.Uint64("storage_id", in.StorageID), zap.Error(err))
			continue
		}
		if err := h.Storage.Delete(ctx, in.StorageKey); err != nil {
			logger.Warn("清理过期上传：删除对象失败，留到下一轮", zap.Uint64("intent_id", in.ID), zap.String("key", in.StorageKey), zap.Error(err))
			continue
		}
		if err := s.intents.Delete(ctx, in.ID); err != nil {
			logger.Warn("清理过期上传：删除意图失败", zap.Uint64("intent_id", in.ID), zap.String("id", strconv.FormatUint(in.ID, 10)), zap.Error(err))
			continue
		}
		cleaned++
	}
	return cleaned, nil
}
