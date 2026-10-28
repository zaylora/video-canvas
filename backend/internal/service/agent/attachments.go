package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"video-canvas/internal/pkg/errcode"
)

// maxAttachments 是一条消息最多带几张图片附件，和前端的 MAX_ATTACH_IMAGES 一致。
const maxAttachments = 9

// AssetLinker 给素材算稳定访问地址（/files/<key>）。素材服务实现它；为 nil 时用不带站点前缀的 /files/<key>。
type AssetLinker interface {
	FileURL(key string) string
}

// AttachmentCheck 是校验通过的一张附件图：展示信息落进 message.user 事件，Image 交给模型。
type AttachmentCheck struct {
	AssetID  string `json:"asset_id"`
	Name     string `json:"name"`      // 展示名：原文件名，没有就是「图片 N」
	MimeType string `json:"mime_type"` // image/png 等
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	URL      string `json:"url"` // 稳定地址（对象存储在访问时现签名跳转，永不过期）；不是签名直链
	// Image 是交给模型的内容：对象存储给地址，本地磁盘给 base64（单张 ≤ 5 MB），规则同看图工具
	Image ToolImage `json:"image"`
}

// CheckAttachments 校验消息的图片附件：素材必须属于该用户、是 png/jpeg/webp/gif，一条消息最多 9 张，
// 本地存储的单张不超过 5 MB。同一个素材只留一次（保持首次出现的顺序）。
// 不合法返回 10001，文案点名是第几张（「图 2：…」），不创建运行。
func (b *AgentBridge) CheckAttachments(ctx context.Context, userID uint64, assetIDs []string) ([]AttachmentCheck, error) {
	if userID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少 user_id")
	}
	if len(assetIDs) > maxAttachments {
		return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("一条消息最多 %d 张图片", maxAttachments))
	}
	out := make([]AttachmentCheck, 0, len(assetIDs))
	seen := make(map[string]bool, len(assetIDs))
	for i, id := range assetIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		img, asset, err := b.loadImageAsset(ctx, userID, id)
		if err != nil {
			if errors.Is(err, errcode.ErrInternal) {
				return nil, err
			}
			return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("图 %d：%s", i+1, err.Error()))
		}
		name := asset.FileName
		if name == "" {
			name = fmt.Sprintf("图片 %d", i+1)
		}
		out = append(out, AttachmentCheck{
			AssetID: strconv.FormatUint(asset.ID, 10), Name: name, MimeType: asset.MimeType,
			Width: asset.Width, Height: asset.Height, URL: b.fileURL(asset.StorageKey), Image: img,
		})
	}
	return out, nil
}

// fileURL 是素材的稳定访问地址。
func (b *AgentBridge) fileURL(key string) string {
	if b.d.AssetLinks != nil {
		return b.d.AssetLinks.FileURL(key)
	}
	return "/files/" + key
}
