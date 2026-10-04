package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

const (
	checkTimeout  = 25 * time.Second
	fetchMaxBytes = 16 << 20 // 试跑最多读这么多字节来统计体积，缩略图远小于它
)

// 校验项状态。
const (
	checkOK   = "ok"
	checkWarn = "warn"
	checkFail = "fail"
)

// CheckItem 是校验与试跑里的一项结果。
type CheckItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Status  string `json:"status"` // ok / warn / fail
	Message string `json:"message"`
}

// TrialImage 是图片缩略图试跑的结果。
type TrialImage struct {
	Bytes       int64 `json:"bytes"`
	MS          int64 `json:"ms"`
	SourceBytes int64 `json:"source_bytes"`
}

// TrialVideo 是视频封面试跑的结果。
type TrialVideo struct {
	Bytes int64 `json:"bytes"`
	MS    int64 `json:"ms"`
}

// ProcessorTrial 是试跑结果：用该存储里的真实素材各取一次，没有素材（或不支持）时对应项为 nil。
type ProcessorTrial struct {
	Image *TrialImage `json:"image"`
	Video *TrialVideo `json:"video"`
}

// ProcessorCheck 是最近一次校验与试跑的结果。
type ProcessorCheck struct {
	OK        bool           `json:"ok"`      // 没有 fail 即为 true（warn 不挡发布）
	Version   int            `json:"version"` // 校验针对的处理服务 version；保存草稿后会落后
	CheckedAt time.Time      `json:"checked_at"`
	Checks    []CheckItem    `json:"checks"`
	Trial     ProcessorTrial `json:"trial"`
}

// FetchResult 是试跑请求处理地址的结果。
type FetchResult struct {
	Status      int    // HTTP 状态码
	Bytes       int64  // 读到的字节数
	ContentType string // 响应的 Content-Type
}

// ProcessorFetcher 请求一个处理地址并统计响应，试跑与域名可达性检查用它。
type ProcessorFetcher interface {
	// Fetch 发起 GET 请求（跟随有限次重定向），网络层失败返回 error，HTTP 非 2xx 不算 error。
	Fetch(ctx context.Context, rawURL string) (FetchResult, error)
}

// httpProcessorFetcher 用带内网地址防护的 HTTP 客户端发请求：域名由管理员填写，不能让它被用来探测内网。
type httpProcessorFetcher struct{ client *http.Client }

// NewHTTPProcessorFetcher 创建默认的试跑抓取器。
func NewHTTPProcessorFetcher() ProcessorFetcher {
	cfg := &netguard.Config{}
	cfg.ApplyDefaults()
	return &httpProcessorFetcher{client: &http.Client{
		Transport: netguard.NewTransport(cfg),
		Timeout:   20 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > cfg.MaxRedirects {
				return netguard.ErrTooManyRedirects
			}
			return nil
		},
	}}
}

// Fetch 实现 ProcessorFetcher。
func (f *httpProcessorFetcher) Fetch(ctx context.Context, rawURL string) (FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchResult{}, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, fetchMaxBytes))
	return FetchResult{Status: resp.StatusCode, Bytes: n, ContentType: resp.Header.Get("Content-Type")}, nil
}

// decodeCheck 解析库里存的校验结果，空值或解析失败返回 nil（按“没校验过”处理）。
func decodeCheck(raw model.JSONText) *ProcessorCheck {
	if len(raw) == 0 {
		return nil
	}
	var c ProcessorCheck
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil
	}
	return &c
}

// Check 对当前草稿做校验与试跑，并把结果存下来供发布使用。校验没通过也返回 200（看 check.ok），
// 因为这是“检查结果”而不是“请求失败”。校验用的是和线上同一套地址生成逻辑，所以试跑通过就说明线上会通。
func (s *ImageProcessorService) Check(ctx context.Context, actorID, id uint64) (*ProcessorView, error) {
	// 1. 加载处理服务、存储与配置
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	st, err := s.storages.GetByID(ctx, p.StorageID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errStorageGone()
		}
		return nil, err
	}
	var cfg imageproc.Config
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		return nil, fmt.Errorf("解析处理服务 %d 的配置失败: %w", p.ID, err)
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	// 2. 逐项检查
	chk := &ProcessorCheck{Version: p.Version, CheckedAt: s.now(), Checks: []CheckItem{}}
	s.runChecks(ctx, chk, p, st, cfg)
	chk.OK = true
	for _, it := range chk.Checks {
		if it.Status == checkFail {
			chk.OK = false
		}
	}

	// 3. 保存结果（不改 version），再返回最新视图
	raw, err := json.Marshal(chk)
	if err != nil {
		return nil, fmt.Errorf("序列化校验结果失败: %w", err)
	}
	if err := s.repo.SetCheck(ctx, id, raw); err != nil {
		return nil, mapProcessorErr(err)
	}
	return s.Get(ctx, id)
}

func errStorageGone() error { return mapProcessorErr(repository.ErrNotFound) }

// runChecks 依次执行各校验项，结果追加进 chk。绑定关系不通过时后面的项没有意义，直接结束。
func (s *ImageProcessorService) runChecks(ctx context.Context, chk *ProcessorCheck, p *model.ImageProcessor, st *model.StorageConfig, cfg imageproc.Config) {
	add := func(key, label, status, msg string) {
		chk.Checks = append(chk.Checks, CheckItem{Key: key, Label: label, Status: status, Message: msg})
	}

	// 1. 绑定关系
	if err := checkProcessorBinding(p.Vendor, st); err != nil {
		add("binding", "绑定关系：存储与处理服务匹配", checkFail, bindingMsg(err))
		return
	}
	add("binding", "绑定关系：存储与处理服务匹配", checkOK, fmt.Sprintf("%s（%s）", st.Name, storageProviderName(st.Provider)))

	// 2. 厂商专属项
	switch p.Vendor {
	case imageproc.VendorCloudflare:
		if host := hostOf(st.PublicBaseURL); host != cfg.Domain {
			add("public_domain", "域名与存储“公开访问域名”一致", checkFail, fmt.Sprintf("存储的公开访问域名是 %s，这里填的是 %s", host, cfg.Domain))
		} else {
			add("public_domain", "域名与存储“公开访问域名”一致", checkOK, cfg.Domain)
		}
	case imageproc.VendorAliyunOSSImg:
		if strings.HasSuffix(cfg.Domain, ".aliyuncs.com") {
			add("custom_domain", "已使用自定义域名", checkWarn, "默认域名下图片预览会变成下载，建议给 Bucket 绑定自定义域名")
		} else {
			add("custom_domain", "已使用自定义域名", checkOK, cfg.Domain)
		}
	case imageproc.VendorTencentCI:
		if cfg.MediaEnabled {
			add("media", "数据万象“媒体处理”已开通", checkOK, "视频封面可用")
		} else {
			add("media", "数据万象“媒体处理”已开通", checkWarn, "未开通：视频封面不可用，视频节点显示占位")
		}
	}

	// 3. 取存储句柄（含凭证）：私有读桶的签名与试跑都要用
	h, err := s.handles.Get(ctx, p.StorageID)
	if err != nil {
		add("sign", "读取存储配置", checkFail, "无法读取存储配置："+err.Error())
		return
	}
	if p.Vendor != imageproc.VendorCloudflare {
		switch {
		case st.PublicBaseURL != "":
			add("sign", "私有读签名", checkOK, "存储已设置公开访问域名，按公开读处理，无需签名")
		case h.Spec.AccessKey == "" || h.Spec.SecretKey == "":
			add("sign", "私有读签名", checkFail, "存储缺少 AccessKey / Secret，无法给处理地址签名")
		default:
			add("sign", "私有读签名", checkOK, "使用存储配置里的 AccessKey 签名，处理参数已签入")
		}
	}

	// 4. 域名可访问：只要能连上（任何 HTTP 状态）就算通，连不上才是域名或网络有问题
	root := "https://" + cfg.Domain + "/"
	if res, err := s.fetcher.Fetch(ctx, root); err != nil {
		add("domain", "访问域名可访问", checkFail, fmt.Sprintf("%s 无法访问：%v", cfg.Domain, err))
	} else {
		add("domain", "访问域名可访问", checkOK, fmt.Sprintf("%s（HTTP %d）", cfg.Domain, res.Status))
	}

	// 5. 试跑：用该存储里真实的图片、视频素材各取一次
	prov, _ := imageproc.New(p.Vendor)
	chk.Trial.Image = s.trial(ctx, add, prov, h, cfg, "image", imageproc.VariantThumb, "trial_image", "试跑：图片缩略图")
	if v := s.trial(ctx, add, prov, h, cfg, "video", imageproc.VariantPoster, "trial_video", "试跑：视频封面"); v != nil {
		chk.Trial.Video = &TrialVideo{Bytes: v.Bytes, MS: v.MS}
	}
}

// trial 对一个素材种类做一次试跑并追加结果项；成功返回体积与耗时，其他情况返回 nil。
func (s *ImageProcessorService) trial(ctx context.Context, add func(key, label, status, msg string), prov imageproc.Provider, h *storage.Handle, cfg imageproc.Config,
	kind, variant, key, label string) *TrialImage {
	// 1. 厂商 / 配置不支持这个变体：不是错误，线上会自动回退
	if !prov.Supports(cfg, variant) {
		add(key, label, checkWarn, "该配置不支持这个变体，线上会回退（图片回原图，视频显示占位）")
		return nil
	}
	// 2. 需要一个真实素材；存储里还没有就没法试跑
	asset, err := s.repo.SampleAsset(ctx, h.ID, kind)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			add(key, label, checkWarn, "这套存储里还没有"+kindLabel(kind)+"素材，没法试跑")
			return nil
		}
		add(key, label, checkFail, "读取试跑素材失败："+err.Error())
		return nil
	}
	// 3. 生成地址并请求，要求 200 且返回的是图片
	u, err := prov.VariantURL(ctx, imageproc.URLInput{Spec: h.Spec, Config: cfg, Key: asset.StorageKey, Variant: variant, TTL: h.SignedTTL})
	if err != nil {
		add(key, label, checkFail, "生成处理地址失败："+err.Error())
		return nil
	}
	start := time.Now()
	res, err := s.fetcher.Fetch(ctx, u)
	ms := time.Since(start).Milliseconds()
	switch {
	case err != nil:
		add(key, label, checkFail, "请求失败："+err.Error())
		return nil
	case res.Status != http.StatusOK:
		add(key, label, checkFail, fmt.Sprintf("处理地址返回 HTTP %d", res.Status))
		return nil
	case !strings.HasPrefix(strings.ToLower(res.ContentType), "image/"):
		add(key, label, checkFail, fmt.Sprintf("返回的不是图片（Content-Type: %s）", res.ContentType))
		return nil
	}
	add(key, label, checkOK, fmt.Sprintf("%s → %s · %d ms", formatBytes(asset.ByteSize), formatBytes(res.Bytes), ms))
	return &TrialImage{Bytes: res.Bytes, MS: ms, SourceBytes: asset.ByteSize}
}

func kindLabel(kind string) string {
	if kind == "video" {
		return "视频"
	}
	return "图片"
}

// bindingMsg 取业务错误里写给管理员看的原因（不带错误码前缀）。
func bindingMsg(err error) string {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Msg
	}
	return err.Error()
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// formatBytes 把字节数转成人能读的单位。
func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dKB", n>>10)
	}
	return fmt.Sprintf("%dB", n)
}
