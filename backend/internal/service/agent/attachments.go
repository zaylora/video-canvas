package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
)

// maxAttachments 是一条消息最多带几张图片附件，和前端的 MAX_ATTACH_IMAGES 一致。
const maxAttachments = 9

// maxDocumentAttachments 是一条消息最多带几个文本文件（.txt / .md），和图片分开计数。
const maxDocumentAttachments = 3

// maxDocumentBytes 是单个文本文件的大小上限，和素材上传时的上限一致。
const maxDocumentBytes = 200_000

// AssetLinker 给素材算稳定访问地址（/files/<key>）。素材服务实现它；为 nil 时用不带站点前缀的 /files/<key>。
type AssetLinker interface {
	FileURL(key string) string
}

// 附件种类。
const (
	AttachmentImage    = "image"    // 图片：交给模型看图
	AttachmentDocument = "document" // 文本文件（.txt / .md）：全文交给模型
)

// AttachmentCheck 是校验通过的一个附件：展示信息落进 message.user 事件，图片的 Image 或文档的 Text 交给模型。
type AttachmentCheck struct {
	AssetID  string `json:"asset_id"`
	Kind     string `json:"kind"`      // image / document
	Name     string `json:"name"`      // 展示名：原文件名，没有就是「图片 N」
	MimeType string `json:"mime_type"` // image/png、text/plain 等
	ByteSize int64  `json:"byte_size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	URL      string `json:"url"` // 稳定地址（对象存储在访问时现签名跳转，永不过期）；不是签名直链
	// Image 是图片交给模型的内容：对象存储给地址，本地磁盘给 base64（单张 ≤ 5 MB），规则同看图工具；文档没有
	Image *ToolImage `json:"image,omitempty"`
	// Text 是文档交给模型的全文（UTF-8，去掉开头的 BOM）；图片没有
	Text string `json:"text,omitempty"`
}

// CheckAttachments 校验消息的附件。图片：素材必须属于该用户、是 png/jpeg/webp/gif，最多 9 张，本地存储的单张不超过 5 MB；
// 文本文件（.txt / .md）：最多 3 个，每个不超过 20 万字节、内容是合法的 UTF-8 文本，全文读出来交给模型。
// 同一个素材只留一次（保持首次出现的顺序）。
// 不合法返回 10001，文案点名是第几张 / 第几个（「图 2：…」「文件 1：…」），不创建运行。
func (b *AgentBridge) CheckAttachments(ctx context.Context, userID uint64, assetIDs []string) ([]AttachmentCheck, error) {
	if userID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少 user_id")
	}
	docAssets, err := b.classifyAttachments(ctx, userID, assetIDs)
	if err != nil {
		return nil, err
	}
	out := make([]AttachmentCheck, 0, len(assetIDs))
	seen := make(map[string]bool, len(assetIDs))
	for i, id := range assetIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var (
			att AttachmentCheck
			err error
		)
		if docAssets[i] != nil {
			att, err = b.checkDocument(ctx, userID, docAssets[i], i+1)
		} else {
			att, err = b.checkImage(ctx, userID, id, i+1)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, att)
	}
	return out, nil
}

// classifyAttachments 先按提交的个数数一遍（重复的也算）：图片和文本文件各有各的上限，互不占名额。
// 返回的切片与 assetIDs 一一对应，文档素材有值，图片（和不存在的）是 nil。
func (b *AgentBridge) classifyAttachments(ctx context.Context, userID uint64, assetIDs []string) ([]*model.Asset, error) {
	docAssets := make([]*model.Asset, len(assetIDs))
	images, docs := 0, 0
	for i, id := range assetIDs {
		if docAssets[i] = b.documentAsset(ctx, userID, id); docAssets[i] != nil {
			docs++
		} else {
			images++
		}
	}
	if images > maxAttachments {
		return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("一条消息最多 %d 张图片", maxAttachments))
	}
	if docs > maxDocumentAttachments {
		return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("一条消息最多 %d 个文本文件", maxDocumentAttachments))
	}
	return docAssets, nil
}

// checkImage 校验一张附件图。n 是它在附件里的位置（从 1 数），用于报错。
func (b *AgentBridge) checkImage(ctx context.Context, userID uint64, assetID string, n int) (AttachmentCheck, error) {
	img, asset, err := b.loadImageAsset(ctx, userID, assetID)
	if err != nil {
		if errors.Is(err, errcode.ErrInternal) {
			return AttachmentCheck{}, err
		}
		return AttachmentCheck{}, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("图 %d：%s", n, err.Error()))
	}
	name := asset.FileName
	if name == "" {
		name = fmt.Sprintf("图片 %d", n)
	}
	return AttachmentCheck{
		AssetID: strconv.FormatUint(asset.ID, 10), Kind: AttachmentImage, Name: name, MimeType: asset.MimeType,
		Width: asset.Width, Height: asset.Height, URL: b.fileURL(asset.StorageKey), Image: &img,
	}, nil
}

// documentAsset 返回 id 对应的文档素材；不是数字、不存在、不是这个用户的、不是文档都返回 nil（交给图片流程报错，文案不变）。
func (b *AgentBridge) documentAsset(ctx context.Context, userID uint64, assetID string) *model.Asset {
	id, err := strconv.ParseUint(assetID, 10, 64)
	if err != nil || b.d.Assets == nil {
		return nil
	}
	a, err := b.d.Assets.Get(ctx, userID, id)
	if err != nil || a == nil || a.Kind != "document" {
		return nil
	}
	return a
}

// checkDocument 把文档素材读成全文：大小在上限内、是合法的 UTF-8 文本（去掉开头的 BOM）。n 是它在附件里的位置（从 1 数），用于报错。
func (b *AgentBridge) checkDocument(ctx context.Context, userID uint64, asset *model.Asset, n int) (AttachmentCheck, error) {
	bad := func(msg string) (AttachmentCheck, error) {
		return AttachmentCheck{}, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("文件 %d：%s", n, msg))
	}
	if asset.ByteSize > maxDocumentBytes {
		return bad(fmt.Sprintf("太大了（%d 字节），请控制在 %d 字节以内", asset.ByteSize, maxDocumentBytes))
	}
	f, err := b.d.Assets.Open(ctx, userID, asset.ID)
	if errors.Is(err, provider.ErrAssetNotFound) {
		return bad("文件不存在或已删除")
	}
	if err != nil {
		return AttachmentCheck{}, errcode.ErrInternal
	}
	defer f.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(f.Body, maxDocumentBytes+1))
	if err != nil {
		return AttachmentCheck{}, errcode.ErrInternal
	}
	if len(raw) > maxDocumentBytes || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return bad("不是有效的文本文件（需要 UTF-8 编码的 .txt 或 .md）")
	}
	name := asset.FileName
	if name == "" {
		name = fmt.Sprintf("文件 %d", n)
	}
	return AttachmentCheck{
		AssetID: strconv.FormatUint(asset.ID, 10), Kind: AttachmentDocument, Name: name, MimeType: asset.MimeType,
		ByteSize: asset.ByteSize, URL: b.fileURL(asset.StorageKey), Text: strings.TrimPrefix(string(raw), "\uFEFF"),
	}, nil
}

// fileURL 是素材的稳定访问地址。
func (b *AgentBridge) fileURL(key string) string {
	if b.d.AssetLinks != nil {
		return b.d.AssetLinks.FileURL(key)
	}
	return "/files/" + key
}
