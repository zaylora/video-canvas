package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/storage"
)

// 真桶集成测试：对一家真实的对象存储跑完整链路。默认跳过，设置环境变量后才运行，用来在上线前验证
// OSS / COS / S3 / R2 的真实行为（本地假 S3 验证不了签名、POST Policy、预签名 PUT 这些细节）。
//
// 用法（以 OSS 为例；用只授予该测试桶读写权限的子账号密钥，测试对象写在带时间戳的前缀下并在结束时删除）：
//
//	STORAGE_IT_PROVIDER=aliyun_oss STORAGE_IT_REGION=cn-hangzhou STORAGE_IT_BUCKET=my-bucket \
//	STORAGE_IT_ACCESS_KEY=... STORAGE_IT_SECRET_KEY=... \
//	go test ./internal/tests/storage/ -run TestRealBucket -v
//
// 其他变量：STORAGE_IT_ACCOUNT_ID（R2 必填）、STORAGE_IT_ENDPOINT（S3 自定义 endpoint）、
// STORAGE_IT_PUBLIC_BASE_URL（公开桶 / CDN）。密钥只通过环境变量传入，不要写进仓库。
// 浏览器直传还需要桶上配置 CORS，但这里是服务端发请求，不受 CORS 影响，CORS 要在真实浏览器里另测。

func realBucketSpec(t *testing.T) Spec {
	t.Helper()
	provider := os.Getenv("STORAGE_IT_PROVIDER")
	if provider == "" {
		t.Skip("未设置 STORAGE_IT_PROVIDER，跳过真桶集成测试")
	}
	sp := Spec{
		Provider: provider, Region: os.Getenv("STORAGE_IT_REGION"), AccountID: os.Getenv("STORAGE_IT_ACCOUNT_ID"),
		Endpoint: os.Getenv("STORAGE_IT_ENDPOINT"), Bucket: os.Getenv("STORAGE_IT_BUCKET"),
		AccessKey: os.Getenv("STORAGE_IT_ACCESS_KEY"), SecretKey: os.Getenv("STORAGE_IT_SECRET_KEY"),
		PublicBaseURL: os.Getenv("STORAGE_IT_PUBLIC_BASE_URL"), UseSSL: true,
		PathPrefix: fmt.Sprintf("it-%d", time.Now().Unix()),
	}
	if sp.Bucket == "" || sp.AccessKey == "" || sp.SecretKey == "" {
		t.Fatal("需要设置 STORAGE_IT_BUCKET / STORAGE_IT_ACCESS_KEY / STORAGE_IT_SECRET_KEY")
	}
	return sp
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func httpDo(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestRealBucket(t *testing.T) {
	spec := realBucketSpec(t)
	ctx := context.Background()
	st, err := NewFromSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	data := tinyPNG(t)

	t.Run("探针：鉴权与桶 → 写 → 读 → 签名地址访问 → 删", func(t *testing.T) {
		res := Probe(ctx, st, ProbeOptions{})
		for _, s := range res.Steps {
			t.Logf("步骤 %d %-12s ok=%v skipped=%v %dms %+v", s.Index, s.Name, s.OK, s.Skipped, s.DurationMs, s.Issue)
		}
		if !res.OK {
			t.Fatal("探针未通过，后续结果仅供参考")
		}
	})

	t.Run("签名地址：GET 返回内容，带 Range 返回 206", func(t *testing.T) {
		key := "u1/it/read.png"
		if err := st.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "image/png"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Delete(ctx, key) })
		u, err := st.URL(ctx, key, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		resp, body := httpDo(t, req)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, data) {
			t.Fatalf("GET：%d，内容一致=%v", resp.StatusCode, bytes.Equal(body, data))
		}
		req, _ = http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("Range", "bytes=0-3")
		if resp, body = httpDo(t, req); resp.StatusCode != http.StatusPartialContent || len(body) != 4 {
			t.Errorf("Range：%d，读到 %d 字节（视频拖动播放依赖它）", resp.StatusCode, len(body))
		}
	})

	t.Run("Stat 与按范围读取", func(t *testing.T) {
		key := "u1/it/stat.png"
		_ = st.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "image/png")
		t.Cleanup(func() { _ = st.Delete(ctx, key) })
		info, err := st.Stat(ctx, key)
		if err != nil || info.Size != int64(len(data)) {
			t.Fatalf("Stat：%+v err=%v", info, err)
		}
		rc, err := st.OpenRange(ctx, key, 0, 8)
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		if b, _ := io.ReadAll(rc); !bytes.Equal(b, data[:8]) {
			t.Errorf("OpenRange 读到的头 8 字节不对：%x", b)
		}
	})

	t.Run("浏览器直传凭证：真实上传、复核大小", func(t *testing.T) {
		key := "u1/it/direct.png"
		t.Cleanup(func() { _ = st.Delete(ctx, key) })
		d, err := st.DirectUpload(ctx, DirectUploadRequest{Key: key, ContentType: "image/png", Size: int64(len(data)), MaxSize: 1 << 20, TTL: 5 * time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("直传方式：%s", d.Method)
		upload(t, d, data)
		info, err := st.Stat(ctx, key)
		if err != nil || info.Size != int64(len(data)) {
			t.Fatalf("上传后 Stat：%+v err=%v", info, err)
		}
	})

	t.Run("直传的大小限制：超限的上传应被拒绝", func(t *testing.T) {
		key := "u1/it/oversize.png"
		t.Cleanup(func() { _ = st.Delete(ctx, key) })
		d, err := st.DirectUpload(ctx, DirectUploadRequest{Key: key, ContentType: "image/png", Size: int64(len(data)), MaxSize: int64(len(data)), TTL: 5 * time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		big := append(append([]byte{}, data...), bytes.Repeat([]byte{0}, 4096)...)
		status := tryUpload(t, d, big)
		_, statErr := st.Stat(ctx, key)
		switch d.Method {
		case "post":
			// POST Policy 的 content-length-range 在桶侧强制，必须被拒绝
			if status < 400 || statErr == nil {
				t.Errorf("POST Policy 应拒绝超限上传：HTTP %d，对象已存在=%v", status, statErr == nil)
			}
		default:
			// 预签名 PUT 把 Content-Length 签进了地址：服务端若校验签名头，超限（与签名不符）的请求会被拒绝。
			// 不同服务商行为不一，只记录结果；就算被接受，登记时的 Stat 复核也会删除它
			t.Logf("预签名 PUT 上传了与签名不符的大小：HTTP %d，对象已存在=%v（已存在说明服务端没有强制 Content-Length，复核是唯一防线）", status, statErr == nil)
		}
	})

	if spec.PublicBaseURL != "" {
		t.Run("公开地址：不带签名可直接访问", func(t *testing.T) {
			key := "u1/it/public.png"
			_ = st.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "image/png")
			t.Cleanup(func() { _ = st.Delete(ctx, key) })
			u, _ := st.URL(ctx, key, time.Minute)
			if parsed, _ := url.Parse(u); parsed.RawQuery != "" {
				t.Errorf("公开地址不应带签名参数：%s", u)
			}
			req, _ := http.NewRequest(http.MethodGet, u, nil)
			if resp, _ := httpDo(t, req); resp.StatusCode != http.StatusOK {
				t.Errorf("公开地址返回 HTTP %d（桶没开公开读，或域名没指向桶）", resp.StatusCode)
			}
		})
	}
}

// upload 按直传凭证真实上传，要求 2xx。
func upload(t *testing.T, d *DirectUpload, data []byte) {
	t.Helper()
	if status := tryUpload(t, d, data); status < 200 || status > 299 {
		t.Fatalf("直传返回 HTTP %d", status)
	}
}

// tryUpload 按直传凭证上传并返回 HTTP 状态码。
func tryUpload(t *testing.T, d *DirectUpload, data []byte) int {
	t.Helper()
	var req *http.Request
	if d.Method == "post" {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range d.Fields {
			_ = mw.WriteField(k, v)
		}
		part, _ := mw.CreateFormFile("file", "x.png") // file 字段必须放在最后
		_, _ = part.Write(data)
		_ = mw.Close()
		req, _ = http.NewRequest(http.MethodPost, d.URL, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
	} else {
		req, _ = http.NewRequest(http.MethodPut, d.URL, bytes.NewReader(data))
		for k, v := range d.Headers {
			req.Header.Set(k, v)
		}
	}
	resp, body := httpDo(t, req)
	if resp.StatusCode >= 400 {
		t.Logf("上传响应 HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.StatusCode
}
