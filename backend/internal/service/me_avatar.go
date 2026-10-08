package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

const (
	// AvatarMaxBytes 是头像文件的大小上限（前端已裁成 512×512 WebP，正常只有几十 KB）。handler 据此限制请求体。
	AvatarMaxBytes = 2 << 20
	// avatarMaxSide 是头像宽高的上限：服务端不做缩放，超大尺寸说明没走前端裁剪，直接拒绝。
	avatarMaxSide = 2048
	// AvatarKeyPrefix 是头像对象 key 的前缀：avatars/<user_id>/<16 位随机串>.<扩展名>。/files 据此走头像反查。
	AvatarKeyPrefix = "avatars/"
)

// avatarMimes 是头像允许的真实类型（由内容嗅探决定），是素材白名单里的图片子集。
var avatarMimes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

// UploadAvatar 上传并替换当前用户的头像（POST /me/avatar），返回最新资料。
// 头像不进 assets 表（不混进素材库），直接写入存储并记在 users.avatar_key / avatar_storage_id 上。
func (s *MeService) UploadAvatar(ctx context.Context, userID uint64, body io.Reader) (*model.MeView, error) {
	// 1. 读取最多 2MB+1 字节：多读 1 字节才能区分“恰好 2MB”和“超了”
	data, err := readAvatar(body)
	if err != nil {
		return nil, err
	}

	// 2. 按 magic bytes 嗅探真实类型（复用素材的嗅探），不信任文件名与 Content-Type，防止 txt 改名成 png
	head := data[:min(len(data), assetHeadLen)]
	mime, ok := sniffAssetMime(head)
	if !ok || !avatarMimes[mime] {
		return nil, errcode.ErrAvatarFormat
	}

	// 3. 解码出尺寸：解不出说明文件损坏，按格式不支持处理；超过 2048×2048 按过大处理
	meta := probeAssetMeta(mime, bytes.NewReader(data), int64(len(data)))
	if meta.Width <= 0 || meta.Height <= 0 {
		return nil, errcode.ErrAvatarFormat
	}
	if meta.Width > avatarMaxSide || meta.Height > avatarMaxSide {
		return nil, errcode.ErrAvatarTooLarge.WithMsg(fmt.Sprintf("头像尺寸不能超过 %d×%d", avatarMaxSide, avatarMaxSide))
	}

	// 4. 读旧头像（替换成功后要删掉它）
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 5. 写入当前默认存储；key 不可猜（/files 不鉴权，靠 key 保护），扩展名取自嗅探结果
	h, err := s.Stores.Default(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取默认存储失败: %w", err)
	}
	key := fmt.Sprintf("%s%d/%s%s", AvatarKeyPrefix, userID, randomHex16(), assetMimeExt[mime])
	if err := h.Storage.Put(ctx, key, bytes.NewReader(data), int64(len(data)), mime); err != nil {
		cleanupObject(ctx, h.Storage, key)
		return nil, fmt.Errorf("写入头像失败: %w", err)
	}

	// 6. 更新 avatar_key 与所在存储；失败要删掉刚写的对象，否则留下没人引用的孤儿文件
	if err := s.Repo.Update(ctx, userID, map[string]any{"avatar_key": key, "avatar_storage_id": h.ID}); err != nil {
		cleanupObject(ctx, h.Storage, key)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrUserNotFound
		}
		return nil, err
	}

	// 7. 尽力删除旧头像（失败只记日志：旧 key 已不被引用，/files 也不会再提供它），清缓存
	s.deleteOldAvatar(ctx, u.AvatarStorageID, u.AvatarKey)
	s.Users.InvalidateUser(ctx, userID)
	return s.reloadView(ctx, userID)
}

// DeleteAvatar 移除当前用户的头像（DELETE /me/avatar），返回最新资料；本来就没有头像时直接返回。
func (s *MeService) DeleteAvatar(ctx context.Context, userID uint64) (*model.MeView, error) {
	// 1. 读当前头像；没有头像不必写库
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.AvatarKey == "" {
		return s.meView(u), nil
	}

	// 2. 先清字段再删文件：顺序反过来的话，删文件成功而写库失败会留下指向不存在文件的头像
	if err := s.Repo.Update(ctx, userID, map[string]any{"avatar_key": "", "avatar_storage_id": uint64(0)}); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrUserNotFound
		}
		return nil, err
	}

	// 3. 尽力删除旧文件并清缓存
	s.deleteOldAvatar(ctx, u.AvatarStorageID, u.AvatarKey)
	s.Users.InvalidateUser(ctx, userID)
	return s.reloadView(ctx, userID)
}

// readAvatar 读取头像内容：超过 AvatarMaxBytes 返回 55006；请求体被 http.MaxBytesReader 截断也按过大处理；
// 其他读取失败（客户端中断）按参数错误返回；空文件按格式不支持处理。
func readAvatar(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, errcode.ErrAvatarFormat
	}
	data, err := io.ReadAll(io.LimitReader(body, AvatarMaxBytes+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, avatarTooLarge()
		}
		logger.Warn("读取头像内容失败", zap.Error(err))
		return nil, errcode.ErrInvalidParams.WithMsg("上传中断或读取文件失败，请重试")
	}
	if len(data) > AvatarMaxBytes {
		return nil, avatarTooLarge()
	}
	if len(data) == 0 {
		return nil, errcode.ErrAvatarFormat
	}
	return data, nil
}

// avatarTooLarge 构造“文件超过 2MB”的 55006。
func avatarTooLarge() error {
	return errcode.ErrAvatarTooLarge.WithMsg("头像文件不能超过 2MB")
}

// deleteOldAvatar 尽力删除旧头像对象：按旧头像记录的存储删（默认存储可能已经换了）；没有旧头像、存储已不存在或删除失败都只记日志。
func (s *MeService) deleteOldAvatar(ctx context.Context, storageID uint64, key string) {
	if key == "" {
		return
	}
	h, err := s.Stores.Get(ctx, storageID)
	if err != nil {
		logger.Warn("删除旧头像时找不到所在存储，可能留下孤儿文件", zap.Uint64("storage_id", storageID), zap.String("key", key), zap.Error(err))
		return
	}
	cleanupObject(ctx, h.Storage, key)
}

// randomHex16 生成 16 位十六进制随机串（8 字节熵，crypto/rand），用作头像文件名。
func randomHex16() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("生成随机数失败: " + err.Error()) // crypto/rand 失败意味着系统熵源不可用，无法安全继续
	}
	return hex.EncodeToString(b[:])
}
