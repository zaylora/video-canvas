package imageproc_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/imageproc"

	"video-canvas/internal/storage"
)

func boolPtr(b bool) *bool { return &b }

func mustProvider(t *testing.T, vendor string) Provider {
	t.Helper()
	p, ok := New(vendor)
	if !ok {
		t.Fatalf("未找到厂商 %q 的适配器", vendor)
	}
	return p
}

func TestNewUnknownVendor(t *testing.T) {
	if _, ok := New("aws_dit"); ok {
		t.Fatal("AWS 本期不做，不应有适配器")
	}
}

func TestPresets(t *testing.T) {
	list := Presets()
	if len(list) != 3 {
		t.Fatalf("应有 3 个厂商预设，实际 %d", len(list))
	}
	want := map[string]struct {
		provider string
		public   bool
	}{
		VendorCloudflare:   {storage.ProviderR2, true},
		VendorTencentCI:    {storage.ProviderTencentCOS, false},
		VendorAliyunOSSImg: {storage.ProviderAliyunOSS, false},
	}
	for _, p := range list {
		w, ok := want[p.Vendor]
		if !ok || p.StorageProvider != w.provider || p.RequiresPublicBase != w.public || !p.SupportsPoster {
			t.Errorf("预设 %+v 与预期 %+v 不符", p, w)
		}
		if len(p.Formats) == 0 || p.DefaultConfig.Width != 512 || p.DefaultConfig.Format != p.Formats[0] {
			t.Errorf("预设 %s 的格式或默认配置不对：%+v", p.Vendor, p)
		}
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	tests := []struct {
		name    string
		vendor  string
		in      Config
		wantErr string
		check   func(t *testing.T, got Config)
	}{
		{
			name: "Cloudflare 补默认值", vendor: VendorCloudflare, in: Config{Domain: "assets.example.com"},
			check: func(t *testing.T, g Config) {
				if g.Width != 512 || g.Format != "auto" || g.Quality != 75 || g.OnErrorRedirect == nil || !*g.OnErrorRedirect {
					t.Errorf("实际 %+v", g)
				}
			},
		},
		{
			name: "域名里的协议与路径被拿掉", vendor: VendorTencentCI, in: Config{Domain: "https://a.cos.ap-seoul.myqcloud.com/x/"},
			check: func(t *testing.T, g Config) {
				if g.Domain != "a.cos.ap-seoul.myqcloud.com" {
					t.Errorf("实际 %q", g.Domain)
				}
			},
		},
		{name: "域名必填", vendor: VendorAliyunOSSImg, in: Config{Width: 512}, wantErr: "域名"},
		{name: "长边过小", vendor: VendorAliyunOSSImg, in: Config{Domain: "img.example.com", Width: 8}, wantErr: "长边"},
		{name: "长边过大", vendor: VendorAliyunOSSImg, in: Config{Domain: "img.example.com", Width: 5000}, wantErr: "长边"},
		{name: "格式不在预设里", vendor: VendorTencentCI, in: Config{Domain: "a.example.com", Format: "avif"}, wantErr: "格式"},
		{name: "取帧时间不能为负", vendor: VendorCloudflare, in: Config{Domain: "a.example.com", TimeSec: -1}, wantErr: "取帧"},
		{name: "质量越界", vendor: VendorCloudflare, in: Config{Domain: "a.example.com", Quality: 101}, wantErr: "质量"},
		{name: "域名带非法字符", vendor: VendorCloudflare, in: Config{Domain: "a b.example.com"}, wantErr: "域名"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.vendor, tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("期望错误含 %q，实际 %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错：%v", err)
			}
			tc.check(t, got)
		})
	}
}

func TestCloudflareURL(t *testing.T) {
	p := mustProvider(t, VendorCloudflare)
	spec := storage.Spec{Provider: storage.ProviderR2, PublicBaseURL: "https://assets.example.com"}
	cfg := Config{Domain: "assets.example.com", Width: 512, Format: "auto", Quality: 75, OnErrorRedirect: boolPtr(true)}
	ctx := context.Background()

	tests := []struct {
		name    string
		spec    storage.Spec
		cfg     Config
		key     string
		variant string
		want    string
	}{
		{"缩略图", spec, cfg, "u1/202610/abc.png", VariantThumb,
			"https://assets.example.com/cdn-cgi/image/width=512,fit=scale-down,format=auto,quality=75,onerror=redirect/https://assets.example.com/u1/202610/abc.png"},
		{"视频封面，取帧 0.5 秒", spec, Config{Domain: "assets.example.com", Width: 512, Format: "auto", TimeSec: 0.5}, "u1/a.mp4", VariantPoster,
			"https://assets.example.com/cdn-cgi/media/mode=frame,time=0.5s,width=512,format=jpg/https://assets.example.com/u1/a.mp4"},
		{"路径前缀与特殊字符", storage.Spec{Provider: storage.ProviderR2, PublicBaseURL: "https://assets.example.com", PathPrefix: "/media/"}, cfg,
			"u1/a b.png", VariantThumb,
			"https://assets.example.com/cdn-cgi/image/width=512,fit=scale-down,format=auto,quality=75,onerror=redirect/https://assets.example.com/media/u1/a%20b.png"},
		{"公开地址自带路径时 zone 仍是域名", storage.Spec{Provider: storage.ProviderR2, PublicBaseURL: "https://assets.example.com/pub"}, cfg,
			"u1/a.png", VariantThumb,
			"https://assets.example.com/cdn-cgi/image/width=512,fit=scale-down,format=auto,quality=75,onerror=redirect/https://assets.example.com/pub/u1/a.png"},
		{"关闭失败回退原图", spec, Config{Domain: "assets.example.com", Width: 256, Format: "webp", Quality: 80, OnErrorRedirect: boolPtr(false)}, "u1/a.png", VariantThumb,
			"https://assets.example.com/cdn-cgi/image/width=256,fit=scale-down,format=webp,quality=80/https://assets.example.com/u1/a.png"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.VariantURL(ctx, URLInput{Spec: tc.spec, Config: tc.cfg, Key: tc.key, Variant: tc.variant})
			if err != nil || got != tc.want {
				t.Fatalf("\n实际 %q (%v)\n期望 %q", got, err, tc.want)
			}
		})
	}

	t.Run("存储没有公开域名时报错", func(t *testing.T) {
		_, err := p.VariantURL(ctx, URLInput{Spec: storage.Spec{Provider: storage.ProviderR2}, Config: cfg, Key: "u1/a.png", Variant: VariantThumb})
		if err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestTencentURL(t *testing.T) {
	p := mustProvider(t, VendorTencentCI)
	ctx := context.Background()
	host := "canvas-1250000000.cos.ap-seoul.myqcloud.com"
	cfg := Config{Domain: host, Width: 512, Format: "jpg", MediaEnabled: true}
	pub := storage.Spec{Provider: storage.ProviderTencentCOS, Bucket: "canvas-1250000000", Region: "ap-seoul", PublicBaseURL: "https://" + host}
	priv := storage.Spec{Provider: storage.ProviderTencentCOS, Bucket: "canvas-1250000000", Region: "ap-seoul", AccessKey: "AKIDtest", SecretKey: "secret-test"}

	t.Run("公开读：直接拼处理参数，不签名", func(t *testing.T) {
		got, err := p.VariantURL(ctx, URLInput{Spec: pub, Config: cfg, Key: "u1/a.png", Variant: VariantThumb})
		want := "https://" + host + "/u1/a.png?imageMogr2/thumbnail/512x512%3E/format/jpg"
		if err != nil || got != want {
			t.Fatalf("\n实际 %q (%v)\n期望 %q", got, err, want)
		}
		got, err = p.VariantURL(ctx, URLInput{Spec: pub, Config: cfg, Key: "u1/a.mp4", Variant: VariantPoster})
		want = "https://" + host + "/u1/a.mp4?ci-process=snapshot&time=0&width=512&format=jpg"
		if err != nil || got != want {
			t.Fatalf("\n实际 %q (%v)\n期望 %q", got, err, want)
		}
	})

	t.Run("私有读：用 COS SDK 签名并把处理参数签入", func(t *testing.T) {
		got, err := p.VariantURL(ctx, URLInput{Spec: priv, Config: cfg, Key: "u1/a.png", Variant: VariantThumb, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		u, perr := url.Parse(got)
		if perr != nil || u.Host != host || u.Path != "/u1/a.png" {
			t.Fatalf("地址不对：%q", got)
		}
		q := u.Query()
		if q.Get("q-sign-algorithm") != "sha1" || q.Get("q-ak") != "AKIDtest" || q.Get("q-signature") == "" {
			t.Errorf("缺少签名参数：%q", got)
		}
		if _, ok := q["imageMogr2/thumbnail/512x512>/format/jpg"]; !ok {
			t.Errorf("处理参数没有出现在地址里：%q", got)
		}
		if strings.Contains(got, "secret-test") {
			t.Error("地址里不能出现 SecretKey")
		}
	})

	t.Run("私有读封面：处理参数是普通查询参数", func(t *testing.T) {
		got, err := p.VariantURL(ctx, URLInput{Spec: priv, Config: Config{Domain: host, Width: 512, Format: "jpg", TimeSec: 1.5, MediaEnabled: true}, Key: "u1/a.mp4", Variant: VariantPoster, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		q := mustQuery(t, got)
		if q.Get("ci-process") != "snapshot" || q.Get("time") != "1.5" || q.Get("width") != "512" || q.Get("format") != "jpg" || q.Get("q-signature") == "" {
			t.Errorf("实际 %q", got)
		}
	})

	t.Run("未开通媒体处理时不支持封面", func(t *testing.T) {
		if p.Supports(Config{Domain: host, MediaEnabled: false}, VariantPoster) {
			t.Error("未开通媒体处理不应支持封面")
		}
		if !p.Supports(Config{Domain: host, MediaEnabled: false}, VariantThumb) {
			t.Error("缩略图不依赖媒体处理")
		}
	})

	t.Run("私有读缺少凭证时报错", func(t *testing.T) {
		_, err := p.VariantURL(ctx, URLInput{Spec: storage.Spec{Provider: storage.ProviderTencentCOS, Bucket: "b-1250000000", Region: "ap-seoul"}, Config: cfg, Key: "u1/a.png", Variant: VariantThumb})
		if err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestAliyunURL(t *testing.T) {
	p := mustProvider(t, VendorAliyunOSSImg)
	ctx := context.Background()
	cfg := Config{Domain: "img.example.com", Width: 512, Format: "jpg"}
	pub := storage.Spec{Provider: storage.ProviderAliyunOSS, Bucket: "vc-bucket", Region: "cn-hangzhou", PublicBaseURL: "https://img.example.com"}
	priv := storage.Spec{Provider: storage.ProviderAliyunOSS, Bucket: "vc-bucket", Region: "cn-hangzhou", AccessKey: "LTAItest", SecretKey: "secret-test"}

	t.Run("公开读：直接拼 x-oss-process", func(t *testing.T) {
		got, err := p.VariantURL(ctx, URLInput{Spec: pub, Config: cfg, Key: "u1/a.png", Variant: VariantThumb})
		want := "https://img.example.com/u1/a.png?x-oss-process=image/resize,l_512/format,jpg"
		if err != nil || got != want {
			t.Fatalf("\n实际 %q (%v)\n期望 %q", got, err, want)
		}
		got, err = p.VariantURL(ctx, URLInput{Spec: pub, Config: Config{Domain: "img.example.com", Width: 512, Format: "jpg", TimeSec: 1.5}, Key: "u1/a.mp4", Variant: VariantPoster})
		want = "https://img.example.com/u1/a.mp4?x-oss-process=video/snapshot,t_1500,f_jpg,w_512,m_fast"
		if err != nil || got != want {
			t.Fatalf("\n实际 %q (%v)\n期望 %q", got, err, want)
		}
	})

	t.Run("私有读：用 OSS SDK 的 V4 预签名并把 x-oss-process 签入", func(t *testing.T) {
		got, err := p.VariantURL(ctx, URLInput{Spec: priv, Config: cfg, Key: "u1/a.png", Variant: VariantThumb, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(got)
		q := u.Query()
		if u.Host != "img.example.com" || u.Path != "/u1/a.png" {
			t.Fatalf("地址不对：%q", got)
		}
		if q.Get("x-oss-process") != "image/resize,l_512/format,jpg" || q.Get("x-oss-signature") == "" || !strings.Contains(q.Get("x-oss-signature-version"), "OSS4") {
			t.Errorf("缺少 V4 签名参数或处理参数：%q", got)
		}
		if strings.Contains(got, "secret-test") {
			t.Error("地址里不能出现 SecretKey")
		}
	})

	t.Run("私有读封面", func(t *testing.T) {
		got, err := p.VariantURL(ctx, URLInput{Spec: priv, Config: cfg, Key: "u1/a.mp4", Variant: VariantPoster, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if q := mustQuery(t, got); q.Get("x-oss-process") != "video/snapshot,t_0,f_jpg,w_512,m_fast" || q.Get("x-oss-signature") == "" {
			t.Errorf("实际 %q", got)
		}
	})

	t.Run("私有读缺少凭证时报错", func(t *testing.T) {
		_, err := p.VariantURL(ctx, URLInput{Spec: storage.Spec{Provider: storage.ProviderAliyunOSS, Bucket: "vc-bucket", Region: "cn-hangzhou"}, Config: cfg, Key: "u1/a.png", Variant: VariantThumb})
		if err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestUnsupportedVariant(t *testing.T) {
	p := mustProvider(t, VendorCloudflare)
	_, err := p.VariantURL(context.Background(), URLInput{Spec: storage.Spec{Provider: storage.ProviderR2, PublicBaseURL: "https://a.example.com"}, Config: Config{Domain: "a.example.com", Width: 512, Format: "auto"}, Key: "u1/a.png", Variant: "huge"})
	if err == nil {
		t.Fatal("未知变体应报错")
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析 %q 失败：%v", raw, err)
	}
	return u.Query()
}
