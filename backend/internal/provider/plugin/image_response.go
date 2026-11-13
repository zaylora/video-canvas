package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/pluginproto"
)

const maxImageJSONBytes = 64 << 20
const maxInlineImageBytes = 12 << 20
const maxInlineImages = 10

type inlineImage struct {
	item map[string]any
	data []byte
	mime string
}

// imageJSONResponse 在进入插件和追踪之前将内联图片保存为当前任务的素材。
func (o *operation) imageJSONResponse(ctx context.Context, resp *http.Response) (hookResponse, error) {
	o.imageAssets = nil
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageJSONBytes+1))
	if err != nil || len(raw) > maxImageJSONBytes {
		return hookResponse{}, terminalErr("", "读取图片响应失败或响应超过大小上限", nil)
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return hookResponse{}, terminalErr("", "图片响应不是有效 JSON", nil)
	}
	images, err := extractInlineImages(body)
	if err != nil {
		return hookResponse{}, err
	}
	for index, image := range images {
		image.item["asset_index"] = index
	}
	scrubImageData(body)
	sanitized, err := json.Marshal(body)
	if err != nil {
		return hookResponse{}, terminalErr("", "处理图片响应失败", nil)
	}
	response := responseFromBytes(resp, sanitized, responseJSON)
	argument, err := json.Marshal(response)
	if err != nil || len(argument) > pluginproto.MaxPayloadBytes-4096 {
		return hookResponse{}, terminalErr("", "图片响应元数据超过大小上限", nil)
	}
	if len(images) > 0 && o.e.opts.Saver == nil {
		return hookResponse{}, terminalErr(codeHostMisconfigured, "宿主未配置生成素材存储", nil)
	}
	for _, image := range images {
		asset, url, err := o.e.opts.Saver.SaveGenerated(ctx, provider.SaveGeneratedInput{
			UserID: o.task.UserID, TaskID: o.task.ID, Kind: o.modelKind(), MimeType: image.mime,
			Body: bytes.NewReader(image.data),
		})
		if err != nil {
			return hookResponse{}, terminalErr("", "保存生成图片失败", redactError(err, o.red.str))
		}
		o.imageAssets = append(o.imageAssets, provider.Output{Type: provider.OutputAsset, AssetID: asset.ID, AssetURL: url, MediaType: o.modelKind(), Mime: asset.MimeType, DurationMs: asset.DurationMs, Width: asset.Width, Height: asset.Height})
	}
	return response, nil
}

func extractInlineImages(body map[string]any) ([]inlineImage, error) {
	list, _ := body["data"].([]any)
	images := make([]inlineImage, 0)
	for _, value := range list {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		delete(item, "asset_index")
		if url, _ := item["url"].(string); strings.HasPrefix(strings.ToLower(url), "http://") || strings.HasPrefix(strings.ToLower(url), "https://") {
			continue
		}
		encoded, ok := item["b64_json"].(string)
		if !ok || encoded == "" {
			continue
		}
		if len(images) >= maxInlineImages || len(encoded) > base64.StdEncoding.EncodedLen(maxInlineImageBytes) {
			return nil, terminalErr("", "图片数量或大小超过上限", nil)
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) == 0 || len(data) > maxInlineImageBytes {
			return nil, terminalErr("", "图片 base64 格式或大小不合法", nil)
		}
		mime := http.DetectContentType(data)
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
			return nil, terminalErr("", "图片格式不受支持", nil)
		}
		images = append(images, inlineImage{item: item, data: data, mime: mime})
	}
	return images, nil
}

func scrubImageData(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "b64_json" {
				delete(v, key)
			} else {
				scrubImageData(child)
			}
		}
	case []any:
		for _, child := range v {
			scrubImageData(child)
		}
	}
}

// 非 2xx 的错误体也不得把内联图片送入日志或插件。
func scrubImageError(raw []byte) []byte {
	var body any
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	scrubImageData(body)
	sanitized, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	return sanitized
}
