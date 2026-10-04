package imageproc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	cos "github.com/tencentyun/cos-go-sdk-v5"

	"video-canvas/internal/storage"
)

// tencent 适配腾讯云数据万象（COS 桶上的图片处理与视频截帧）。
// 公开读桶直接拼处理参数；私有读桶用 COS SDK 签名，并把处理参数一起签入，防止别人去掉参数直接拿原图。
type tencent struct{}

func (tencent) Supports(cfg Config, variant string) bool {
	switch variant {
	case VariantThumb:
		return true
	case VariantPoster:
		// 视频截帧属于“媒体处理”，要在腾讯云控制台单独开通；没开通时不给封面，前端显示占位
		return cfg.MediaEnabled
	}
	return false
}

func (t tencent) VariantURL(ctx context.Context, in URLInput) (string, error) {
	if !t.Supports(in.Config, in.Variant) {
		return "", fmt.Errorf("%w: %q", ErrUnsupported, in.Variant)
	}
	// 1. 处理参数：缩略图用 imageMogr2（512x512> 表示只缩小、等比放入框内，等价于长边 512）；封面用 ci-process=snapshot
	var processKey string
	query := url.Values{}
	if in.Variant == VariantThumb {
		processKey = fmt.Sprintf("imageMogr2/thumbnail/%dx%d>/format/%s", in.Config.Width, in.Config.Width, in.Config.Format)
		query[processKey] = []string{""}
	} else {
		query.Set("ci-process", "snapshot")
		query.Set("time", strconv.FormatFloat(in.Config.TimeSec, 'f', -1, 64))
		query.Set("width", strconv.Itoa(in.Config.Width))
		query.Set("format", "jpg")
	}

	// 2. 公开读：不需要签名，按官方文档的写法直接拼（> 编码成 %3E）
	if isPublic(in.Spec) {
		return t.publicURL(in, processKey), nil
	}

	// 3. 私有读：COS SDK 的预签名，处理参数作为查询参数一起参与签名
	if err := needCredentials(in.Spec); err != nil {
		return "", err
	}
	bucketURL, err := url.Parse("https://" + in.Config.Domain)
	if err != nil {
		return "", fmt.Errorf("访问域名不合法: %w", err)
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{})
	signed, err := client.Object.GetPresignedURL(ctx, http.MethodGet, storage.ObjectPath(in.Spec.PathPrefix, in.Key),
		in.Spec.AccessKey, in.Spec.SecretKey, ttlOf(in.TTL), &cos.PresignedURLOptions{Query: &query})
	if err != nil {
		return "", fmt.Errorf("腾讯云签名失败: %w", err)
	}
	return signed.String(), nil
}

// publicURL 拼公开读的处理地址。
func (tencent) publicURL(in URLInput, thumbParam string) string {
	base := "https://" + in.Config.Domain + "/" + objectURLPath(in.Spec, in.Key)
	if in.Variant == VariantThumb {
		return fmt.Sprintf("%s?imageMogr2/thumbnail/%dx%d%%3E/format/%s", base, in.Config.Width, in.Config.Width, in.Config.Format)
	}
	return fmt.Sprintf("%s?ci-process=snapshot&time=%s&width=%d&format=jpg", base,
		strconv.FormatFloat(in.Config.TimeSec, 'f', -1, 64), in.Config.Width)
}
