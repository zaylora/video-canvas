package storage

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// 存储服务商。local 是内置的本地磁盘，不走 Spec；其余都是 S3 兼容的对象存储。
const (
	ProviderLocal      = "local"
	ProviderAliyunOSS  = "aliyun_oss"
	ProviderTencentCOS = "tencent_cos"
	ProviderS3         = "s3"
	ProviderR2         = "r2"
)

// 桶寻址方式。
const (
	AddressingAuto    = "auto"    // 交给 minio-go 按 endpoint 判断
	AddressingVirtual = "virtual" // 虚拟主机：{bucket}.{endpoint}
	AddressingPath    = "path"    // 路径：{endpoint}/{bucket}
)

// 浏览器直传方式。
const (
	DirectPostPolicy   = "post_policy"   // POST Policy 表单，桶侧强制大小上限
	DirectPresignedPut = "presigned_put" // 预签名 PUT，Content-Length 签进地址（R2 不支持 PostObject，只能用它）
)

// ErrInvalidSpec 存储配置不合法，消息里写明具体原因，可以直接给管理员看。
var ErrInvalidSpec = errors.New("storage: 存储配置不合法")

// Spec 是创建一个 S3 兼容存储所需的全部信息（密钥已解密）。
type Spec struct {
	Provider      string
	Endpoint      string // host[:port]，可带协议头；OSS / COS / R2 一律按预设推导，只有 S3 允许自定义
	Region        string
	AccountID     string // 仅 R2
	Bucket        string
	PathPrefix    string
	Addressing    string
	UseSSL        bool
	AccessKey     string
	SecretKey     string
	PublicBaseURL string // 公开桶 / CDN 前缀；为空则用签名 URL
}

// Region 是后台表单里可选的一个地域。
type Region struct {
	ID   string `json:"id"`   // 签名与 endpoint 推导用的地域 id，如 cn-hangzhou
	Name string `json:"name"` // 显示名，如 华东1（杭州）
}

// Preset 是一个服务商的固定行为，管理员不用也不能改。
type Preset struct {
	Provider     string   `json:"provider"`
	Name         string   `json:"name"`
	DirectMethod string   `json:"direct_method"` // 浏览器直传方式
	ForceSSL     bool     `json:"force_ssl"`     // 强制 HTTPS
	Addressing   string   `json:"addressing"`    // 固定的寻址方式，auto 表示不固定
	Regions      []Region `json:"regions"`       // 可选地域；R2 没有地域，为空
}

var presets = map[string]Preset{
	ProviderAliyunOSS: {Provider: ProviderAliyunOSS, Name: "阿里云 OSS", DirectMethod: DirectPostPolicy, ForceSSL: true, Addressing: AddressingVirtual,
		Regions: []Region{{"cn-hangzhou", "华东1（杭州）"}, {"cn-shanghai", "华东2（上海）"}, {"cn-beijing", "华北2（北京）"}, {"cn-shenzhen", "华南1（深圳）"}, {"cn-chengdu", "西南1（成都）"}, {"cn-hongkong", "中国香港"}, {"ap-southeast-1", "新加坡"}}},
	ProviderTencentCOS: {Provider: ProviderTencentCOS, Name: "腾讯云 COS", DirectMethod: DirectPostPolicy, ForceSSL: true, Addressing: AddressingVirtual,
		Regions: []Region{{"ap-guangzhou", "广州"}, {"ap-shanghai", "上海"}, {"ap-beijing", "北京"}, {"ap-chengdu", "成都"}, {"ap-hongkong", "中国香港"}, {"ap-singapore", "新加坡"}}},
	ProviderS3: {Provider: ProviderS3, Name: "AWS S3 / 兼容", DirectMethod: DirectPostPolicy, Addressing: AddressingAuto,
		Regions: []Region{{"us-east-1", "美国东部（弗吉尼亚北部）"}, {"us-west-2", "美国西部（俄勒冈）"}, {"ap-northeast-1", "亚太（东京）"}, {"ap-southeast-1", "亚太（新加坡）"}, {"eu-central-1", "欧洲（法兰克福）"}}},
	ProviderR2: {Provider: ProviderR2, Name: "Cloudflare R2", DirectMethod: DirectPresignedPut, ForceSSL: true, Addressing: AddressingPath},
}

// providerOrder 是预设对外展示的顺序。
var providerOrder = []string{ProviderAliyunOSS, ProviderTencentCOS, ProviderS3, ProviderR2}

// PresetOf 返回服务商预设；local 与未知服务商返回 false。
func PresetOf(provider string) (Preset, bool) {
	p, ok := presets[provider]
	return p, ok
}

// Presets 按固定顺序返回全部服务商预设，给后台表单用。
func Presets() []Preset {
	out := make([]Preset, 0, len(providerOrder))
	for _, k := range providerOrder {
		out = append(out, presets[k])
	}
	return out
}

var (
	// bucketRe 是通用桶名规则：小写字母、数字、- 和 .，3-63 位，首尾是字母或数字。
	bucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	// cosBucketRe：COS 的桶名必须带 -<APPID> 后缀（10 位数字）。
	cosBucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-\d{10}$`)
	// r2AccountRe：R2 的 Account ID 是 32 位小写十六进制。
	r2AccountRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w：%s", ErrInvalidSpec, fmt.Sprintf(format, args...))
}

// Normalize 校验并规范化配置，返回补全后的副本；不检查密钥（密钥在创建客户端时检查）。
//
// 规范化规则：
//   - OSS / COS / R2 的 endpoint 一律按预设推导，忽略传入值，并强制 HTTPS；
//   - R2 的 region 固定为 auto，寻址固定为 path；OSS / COS 寻址固定为虚拟主机；
//   - S3 允许自定义 endpoint，协议头决定是否 SSL 并被去掉；没有 endpoint 时按地域推导；
//   - 公开域名去掉末尾 /，路径前缀去掉首尾 /。
func (s Spec) Normalize() (Spec, error) {
	p, ok := PresetOf(s.Provider)
	if !ok {
		return s, invalid("不支持的服务商 %q", s.Provider)
	}

	// 1. 桶名
	if s.Bucket == "" {
		return s, invalid("Bucket 不能为空")
	}
	if !bucketRe.MatchString(s.Bucket) {
		return s, invalid("桶名只能包含小写字母、数字、- 和 .，长度 3-63")
	}
	if s.Provider == ProviderTencentCOS && !cosBucketRe.MatchString(s.Bucket) {
		return s, invalid("COS 桶名需要带 APPID 后缀，例如 canvas-1250000000")
	}

	// 2. endpoint / region / 寻址 / SSL
	if err := s.resolveEndpoint(); err != nil {
		return s, err
	}
	if p.Addressing != AddressingAuto {
		s.Addressing = p.Addressing
	} else if s.Addressing == "" {
		s.Addressing = AddressingAuto
	}
	if s.Addressing != AddressingAuto && s.Addressing != AddressingVirtual && s.Addressing != AddressingPath {
		return s, invalid("不支持的寻址方式 %q", s.Addressing)
	}
	if p.ForceSSL {
		s.UseSSL = true
	}

	// 3. 公开域名与路径前缀
	if s.PublicBaseURL != "" {
		u, err := url.Parse(s.PublicBaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return s, invalid("公开访问域名必须是 http:// 或 https:// 开头的地址")
		}
		s.PublicBaseURL = strings.TrimRight(s.PublicBaseURL, "/")
	}
	s.PathPrefix = strings.Trim(s.PathPrefix, "/")
	if s.PathPrefix != "" {
		if err := ValidateKey(s.PathPrefix); err != nil {
			return s, invalid("路径前缀不合法（只能含字母、数字、. _ - 和 /，且不能以 . 开头）")
		}
	}
	return s, nil
}

// resolveEndpoint 按服务商补全 endpoint 与 region，直接修改 s。
func (s *Spec) resolveEndpoint() error {
	switch s.Provider {
	case ProviderR2:
		if !r2AccountRe.MatchString(s.AccountID) {
			return invalid("Account ID 应为 32 位小写十六进制")
		}
		s.Region = "auto"
		s.Endpoint = s.AccountID + ".r2.cloudflarestorage.com"
	case ProviderAliyunOSS:
		if s.Region == "" {
			return invalid("请选择地域")
		}
		s.Endpoint = "oss-" + s.Region + ".aliyuncs.com"
	case ProviderTencentCOS:
		if s.Region == "" {
			return invalid("请选择地域")
		}
		s.Endpoint = "cos." + s.Region + ".myqcloud.com"
	case ProviderS3:
		ep := strings.TrimSpace(s.Endpoint)
		switch {
		case strings.HasPrefix(ep, "https://"):
			s.UseSSL = true
		case strings.HasPrefix(ep, "http://"):
			s.UseSSL = false
		}
		ep = strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(ep, "https://"), "http://"), "/")
		if ep == "" {
			if s.Region == "" {
				return invalid("请选择地域，或填写自定义 Endpoint")
			}
			ep = "s3." + s.Region + ".amazonaws.com"
		}
		s.Endpoint = ep
	}
	return nil
}
