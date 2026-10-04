package imageproc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// cloudflare 适配 Cloudflare 的 Image Transformations（/cdn-cgi/image）与 Media Transformations（/cdn-cgi/media）。
// 处理发生在访问域名所在的 zone 上，源是 R2 的公开地址，所以不需要签名。
type cloudflare struct{}

func (cloudflare) Supports(_ Config, variant string) bool {
	return variant == VariantThumb || variant == VariantPoster
}

func (cloudflare) VariantURL(_ context.Context, in URLInput) (string, error) {
	if in.Variant != VariantThumb && in.Variant != VariantPoster {
		return "", fmt.Errorf("%w: %q", ErrUnsupported, in.Variant)
	}
	if in.Spec.PublicBaseURL == "" {
		return "", errors.New("存储没有设置公开访问域名，Cloudflare 处理需要它")
	}
	// 1. zone：处理地址挂在访问域名上；源用对象的公开地址（官方保证绝对 URL 可作源）
	zone := "https://" + in.Config.Domain
	base, err := url.Parse(in.Spec.PublicBaseURL)
	if err != nil || base.Host == "" {
		return "", fmt.Errorf("存储的公开访问域名不合法: %q", in.Spec.PublicBaseURL)
	}
	source := strings.TrimRight(in.Spec.PublicBaseURL, "/") + "/" + objectURLPath(in.Spec, in.Key)

	// 2. 缩略图：scale-down 只缩小不放大；onerror=redirect 让处理失败时直接拿原图（只对同 zone 的源有效）
	if in.Variant == VariantThumb {
		opts := []string{
			"width=" + strconv.Itoa(in.Config.Width),
			"fit=scale-down",
			"format=" + in.Config.Format,
			"quality=" + strconv.Itoa(in.Config.Quality),
		}
		if in.Config.OnErrorRedirect != nil && *in.Config.OnErrorRedirect {
			opts = append(opts, "onerror=redirect")
		}
		return zone + "/cdn-cgi/image/" + strings.Join(opts, ",") + "/" + source, nil
	}

	// 3. 视频封面：提取一帧
	return fmt.Sprintf("%s/cdn-cgi/media/mode=frame,time=%ss,width=%d,format=jpg/%s",
		zone, strconv.FormatFloat(in.Config.TimeSec, 'f', -1, 64), in.Config.Width, source), nil
}
