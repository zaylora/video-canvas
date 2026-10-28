package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
)

// 看图的限制。
const (
	maxInspectImages = 4       // 一次最多看几张
	maxInspectBytes  = 5 << 20 // 本地存储下单张图最大字节数（要转 base64 塞进请求）；对象存储交地址，不受此限
)

// inspectMimes 是可以交给模型看的图片格式。
var inspectMimes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

// toolInspectImage 看图：读出图片节点当前的图片，随工具结果交给模型。看图只读，所有任务模式都能用，但模型必须支持看图。
// 图片内容是用户的素材，只在这一轮的上下文里出现，不写进历史（运行时保存历史前会把图片去掉）。
func (b *AgentBridge) toolInspectImage(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		NodeIDs  []string `json:"nodeIds"`
		AssetIDs []string `json:"assetIds"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	if total := len(a.NodeIDs) + len(a.AssetIDs); total == 0 || total > maxInspectImages {
		return nil, fail("nodeIds 和 assetIds 合计需要 1 到 %d 张图片", maxInspectImages)
	}
	snap, _, err := b.resolveModel(ctx, tc.sess.modelKey)
	if err != nil {
		return nil, err
	}
	if !snap.Model.Capabilities.Vision {
		return nil, fail("当前模型不能看图。可以用 canvas_get_state 读提示词来判断，或请用户换一个支持看图的模型。")
	}
	if b.d.Assets == nil {
		return nil, fail("暂时无法读取图片")
	}
	var refs []canvasgraph.ImageRef
	if len(a.NodeIDs) > 0 {
		var err error
		if refs, err = b.d.Canvas.ImageRefs(ctx, tc.run, a.NodeIDs); err != nil {
			return nil, err
		}
	}
	out := &toolOut{summary: fmt.Sprintf("查看 %d 张图片", len(refs)+len(a.AssetIDs)), extra: map[string]any{"node_ids": a.NodeIDs}}
	text := "已附上图片，按顺序是："
	n := 0
	for _, r := range refs {
		img, err := b.loadImage(ctx, tc.run.UserID, r.AssetID)
		if err != nil {
			return nil, fail("节点 %q（%s）：%v", r.NodeID, r.Label, err)
		}
		out.images = append(out.images, img)
		n++
		text += fmt.Sprintf("\n%d. 节点 %s「%s」", n, r.NodeID, r.Label)
	}
	for _, id := range a.AssetIDs {
		img, err := b.loadImage(ctx, tc.run.UserID, id)
		if err != nil {
			return nil, fail("附件图片 asset:%s：%v", id, err)
		}
		out.images = append(out.images, img)
		n++
		text += fmt.Sprintf("\n%d. 附件图片 asset:%s", n, id)
	}
	out.content = text
	return out, nil
}

// loadImage 读一张图：素材必须属于该用户、是支持的图片格式。
// 素材在对象存储里就交回地址，让模型供应商自己去取，不读内容也不受大小限制；
// 在本地磁盘上（供应商够不着）才读出内容转 base64，单张不超过大小上限。
func (b *AgentBridge) loadImage(ctx context.Context, userID uint64, assetID string) (ToolImage, error) {
	img, _, err := b.loadImageAsset(ctx, userID, assetID)
	return img, err
}

// loadImageAsset 和 loadImage 一样读一张图，同时交回素材记录（附件要它的文件名、尺寸和存储 key）。
func (b *AgentBridge) loadImageAsset(ctx context.Context, userID uint64, assetID string) (ToolImage, *model.Asset, error) {
	id, err := strconv.ParseUint(assetID, 10, 64)
	if err != nil {
		return ToolImage{}, nil, errors.New("图片素材无效")
	}
	f, err := b.d.Assets.Open(ctx, userID, id)
	if errors.Is(err, provider.ErrAssetNotFound) {
		return ToolImage{}, nil, errors.New("图片素材不存在或已删除")
	}
	if err != nil {
		return ToolImage{}, nil, errcode.ErrInternal
	}
	defer f.Body.Close()
	if !slices.Contains(inspectMimes, f.Asset.MimeType) {
		return ToolImage{}, nil, fmt.Errorf("图片格式 %s 不支持查看", f.Asset.MimeType)
	}
	if f.Remote && f.URL != "" {
		return ToolImage{MimeType: f.Asset.MimeType, URL: f.URL}, f.Asset, nil
	}
	if f.Asset.ByteSize > maxInspectBytes {
		return ToolImage{}, nil, fmt.Errorf("图片太大（%d MB），超过 %d MB 的不能查看", f.Asset.ByteSize>>20, maxInspectBytes>>20)
	}
	body, err := io.ReadAll(io.LimitReader(f.Body, maxInspectBytes+1))
	if err != nil {
		return ToolImage{}, nil, errcode.ErrInternal
	}
	if len(body) > maxInspectBytes {
		return ToolImage{}, nil, fmt.Errorf("图片太大，超过 %d MB 的不能查看", maxInspectBytes>>20)
	}
	return ToolImage{MimeType: f.Asset.MimeType, Data: base64.StdEncoding.EncodeToString(body)}, f.Asset, nil
}
