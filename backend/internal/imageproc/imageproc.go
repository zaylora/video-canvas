// Package imageproc 是图片处理服务的适配层：云厂商自带的图片缩放 / 视频截帧，
// 通过在素材地址上拼处理参数（私有读桶再带签名）得到缩略图和视频封面。
// 后端只拼地址、不读原图；每个厂商一个适配器，对外都是 Provider。
package imageproc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"video-canvas/internal/storage"
)

// 处理服务厂商。
const (
	VendorCloudflare   = "cloudflare"     // Cloudflare R2：Image / Media Transformations
	VendorTencentCI    = "tencent_ci"     // 腾讯云数据万象（绑定 COS）
	VendorAliyunOSSImg = "aliyun_oss_img" // 阿里云 OSS 图片处理（绑定 OSS）
)

// 素材变体。
const (
	VariantThumb  = "thumb"  // 图片缩略图
	VariantPoster = "poster" // 视频封面（首帧）
)

const (
	minWidth     = 16
	maxWidth     = 2000
	defaultWidth = 512
	defaultTTL   = time.Hour
)

// ErrUnsupported 表示该厂商 / 配置不支持请求的变体，调用方应回退（缩略图回原图，封面回占位）。
var ErrUnsupported = errors.New("imageproc: 不支持该变体")

// Config 是处理服务的配置，各厂商只用到其中一部分，序列化后存进数据库。
type Config struct {
	Domain          string  `json:"domain"`                      // 访问域名，只写 host
	Width           int     `json:"width"`                       // 缩略图 / 封面长边（px）
	Format          string  `json:"format"`                      // 输出格式
	TimeSec         float64 `json:"time_sec"`                    // 视频封面取帧时间（秒）
	Quality         int     `json:"quality,omitempty"`           // 仅 Cloudflare：质量 1–100
	OnErrorRedirect *bool   `json:"on_error_redirect,omitempty"` // 仅 Cloudflare：处理失败回退原图
	MediaEnabled    bool    `json:"media_enabled,omitempty"`     // 仅腾讯云：已开通数据万象“媒体处理”
}

// Preset 描述一个厂商的固定行为，后台表单据此渲染。
type Preset struct {
	Vendor             string   `json:"vendor"`
	Name               string   `json:"name"`
	StorageProvider    string   `json:"storage_provider"`     // 只能绑定这种服务商的存储
	RequiresPublicBase bool     `json:"requires_public_base"` // 存储必须已设置公开域名
	SupportsPoster     bool     `json:"supports_poster"`
	Formats            []string `json:"formats"` // 可选输出格式，第一个是默认
	DefaultConfig      Config   `json:"default_config"`
}

// URLInput 是生成一个变体地址所需的信息。
type URLInput struct {
	Spec    storage.Spec  // 素材所在存储的配置（含密钥，只在内存里用）
	Config  Config        // 处理服务配置（已 Normalize）
	Key     string        // 业务 key（不含路径前缀）
	Variant string        // VariantThumb / VariantPoster
	TTL     time.Duration // 私有读桶的签名有效期，<=0 用 1 小时
}

// Provider 是一个厂商的适配器。
type Provider interface {
	// Supports 判断在这份配置下是否支持某个变体（例如腾讯云未开通媒体处理时没有封面）。
	Supports(cfg Config, variant string) bool
	// VariantURL 返回变体的处理地址；不支持时返回 ErrUnsupported。
	VariantURL(ctx context.Context, in URLInput) (string, error)
}

var presets = []Preset{
	{
		Vendor: VendorCloudflare, Name: "Cloudflare R2", StorageProvider: storage.ProviderR2, RequiresPublicBase: true, SupportsPoster: true,
		Formats: []string{"auto", "webp", "avif", "jpeg"},
	},
	{
		Vendor: VendorTencentCI, Name: "腾讯云数据万象", StorageProvider: storage.ProviderTencentCOS, SupportsPoster: true,
		Formats: []string{"jpg", "webp"},
	},
	{
		Vendor: VendorAliyunOSSImg, Name: "阿里云 OSS 图片处理", StorageProvider: storage.ProviderAliyunOSS, SupportsPoster: true,
		Formats: []string{"jpg", "webp"},
	},
}

func init() {
	yes := true
	for i := range presets {
		presets[i].DefaultConfig = Config{Width: defaultWidth, Format: presets[i].Formats[0]}
		if presets[i].Vendor == VendorCloudflare {
			presets[i].DefaultConfig.Quality = 75
			presets[i].DefaultConfig.OnErrorRedirect = &yes
		}
	}
}

// Presets 返回全部厂商预设，顺序即后台展示顺序。
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

// PresetOf 按厂商返回预设。
func PresetOf(vendor string) (Preset, bool) {
	for _, p := range presets {
		if p.Vendor == vendor {
			return p, true
		}
	}
	return Preset{}, false
}

// New 返回厂商的适配器；未知厂商返回 false。
func New(vendor string) (Provider, bool) {
	switch vendor {
	case VendorCloudflare:
		return cloudflare{}, true
	case VendorTencentCI:
		return tencent{}, true
	case VendorAliyunOSSImg:
		return newAliyun(), true
	}
	return nil, false
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)

// Normalize 补默认值、清洗域名并校验，返回可直接存库 / 用来生成地址的配置。错误信息可以直接给管理员看。
func Normalize(vendor string, c Config) (Config, error) {
	pre, ok := PresetOf(vendor)
	if !ok {
		return c, fmt.Errorf("不支持的处理服务厂商 %q", vendor)
	}

	// 1. 域名：容忍带协议或路径的写法，只保留 host
	c.Domain = strings.TrimSpace(c.Domain)
	c.Domain = strings.TrimPrefix(strings.TrimPrefix(c.Domain, "https://"), "http://")
	if i := strings.IndexAny(c.Domain, "/?#"); i >= 0 {
		c.Domain = c.Domain[:i]
	}
	if c.Domain == "" {
		return c, errors.New("访问域名不能为空")
	}
	if !hostRe.MatchString(c.Domain) {
		return c, errors.New("访问域名不合法，只写域名本身，例如 assets.example.com")
	}

	// 2. 长边、格式、取帧时间
	if c.Width == 0 {
		c.Width = defaultWidth
	}
	if c.Width < minWidth || c.Width > maxWidth {
		return c, fmt.Errorf("缩略图长边需在 %d–%d 之间", minWidth, maxWidth)
	}
	if c.Format == "" {
		c.Format = pre.Formats[0]
	}
	if !contains(pre.Formats, c.Format) {
		return c, fmt.Errorf("输出格式需为 %s 之一", strings.Join(pre.Formats, " / "))
	}
	if c.TimeSec < 0 {
		return c, errors.New("取帧时间不能为负数")
	}

	// 3. 厂商专属字段：Cloudflare 的质量与失败回退；其他厂商不使用，清零避免存下无意义的值
	if vendor == VendorCloudflare {
		if c.Quality == 0 {
			c.Quality = 75
		}
		if c.Quality < 1 || c.Quality > 100 {
			return c, errors.New("图片质量需在 1–100 之间")
		}
		if c.OnErrorRedirect == nil {
			yes := true
			c.OnErrorRedirect = &yes
		}
	} else {
		c.Quality, c.OnErrorRedirect = 0, nil
	}
	if vendor != VendorTencentCI {
		c.MediaEnabled = false
	}
	return c, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// objectURLPath 返回对象在访问域名下的路径（已转义，不带前导 /）。
func objectURLPath(spec storage.Spec, key string) string {
	return storage.ObjectURLPath(spec.PathPrefix, key)
}

func ttlOf(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultTTL
	}
	return d
}

// isPublic 判断存储是否是公开读（配置了公开访问域名）：公开读不需要签名。
func isPublic(spec storage.Spec) bool { return spec.PublicBaseURL != "" }

func needCredentials(spec storage.Spec) error {
	if spec.AccessKey == "" || spec.SecretKey == "" {
		return errors.New("私有读桶缺少 AccessKey / Secret，无法签名")
	}
	return nil
}
