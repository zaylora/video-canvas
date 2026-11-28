package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// referenceURL 仅为本地文件准备临时 URL，直接沿用已有的公网地址。
func (o *operation) referenceURL(ctx context.Context, id uint64, file *provider.AssetFile) (string, error) {
	if file.Remote || !o.referenceURLsEnabled() {
		return file.URL, nil
	}
	signed, err := o.e.opts.ReferenceURLs.PrepareURL(ctx, o.task, file)
	if errors.Is(err, provider.ErrReferenceCacheDisabled) {
		return file.URL, nil
	}
	if err != nil {
		var classified *provider.Error
		if errors.As(err, &classified) {
			return "", classified
		}
		return "", retryableErr("reference_url_unavailable", "准备临时参考素材失败", nil)
	}
	u, err := url.Parse(signed)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", terminalErr("reference_url_invalid", "临时参考素材未返回有效的 HTTPS 地址", nil)
	}
	if o.referenceURLs == nil {
		o.referenceURLs = make(map[uint64]string)
	}
	o.referenceURLs[id] = signed
	o.red.add(signed)
	query := u.Query()
	for _, k := range []string{"X-Amz-Signature", "X-Amz-Credential", "X-Amz-Security-Token"} {
		o.red.add(query.Get(k))
	}
	return signed, nil
}

// referenceURLsEnabled 把首版临时地址限定在生图和视频插件。
func (o *operation) referenceURLsEnabled() bool {
	return o.e.opts.ReferenceURLs != nil && o.model != nil && (o.model.Kind == modelcfg.KindImage || o.model.Kind == modelcfg.KindVideo)
}

// hasPreparedURL 防止带有效期的 URL 被写进可跨重试复用的准备结果。
func (o *operation) hasPreparedURL(raw []byte) bool {
	for _, signed := range o.referenceURLs {
		u, _ := url.Parse(signed) // referenceURL 已经校验过
		if sig := u.Query().Get("X-Amz-Signature"); sig != "" && strings.Contains(string(raw), sig) {
			return true
		}
	}
	return false
}

// resolveForm 在建立发往上游的请求前解析表单中的引用。
func (o *operation) resolveForm(ctx context.Context, fields []formField) ([]formField, error) {
	resolved := make([]formField, 0, len(fields))
	for _, field := range fields {
		if field.ref != nil {
			value, err := o.resolveRef(ctx, *field.ref)
			if err != nil {
				return nil, err
			}
			field.value = fmt.Sprint(value)
		}
		resolved = append(resolved, field)
	}
	return resolved, nil
}
