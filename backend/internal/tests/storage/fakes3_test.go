package storage_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	. "video-canvas/internal/storage"
)

// fakeS3 是只实现 PUT / GET(Range) / HEAD / DELETE 的 path 寻址 S3 服务，用来在不联网的情况下
// 验证存储实现的读写、Range、探针流程。它不校验签名，只校验“请求形状”。
type fakeS3 struct {
	srv *httptest.Server

	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
	types   map[string]string
	ranges  []string // 收到的 Range 头，用于断言只读了需要的字节
	deleted []string

	failCode   string // 非空时所有请求都返回该 S3 错误码（AccessDenied / NoSuchBucket 等）
	failStatus int
}

func newFakeS3(t *testing.T, bucket string) *fakeS3 {
	t.Helper()
	f := &fakeS3{bucket: bucket, objects: map[string][]byte{}, types: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

// host 返回 host:port，用作自定义 endpoint（http，path 寻址）。
func (f *fakeS3) host() string { return strings.TrimPrefix(f.srv.URL, "http://") }

func (f *fakeS3) fail(code string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCode, f.failStatus = code, status
}

func (f *fakeS3) put(key string, body []byte, contentType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = body
	f.types[key] = contentType
}

func (f *fakeS3) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects)
}

func (f *fakeS3) writeErr(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message><Resource>%s</Resource><RequestId>r1</RequestId></Error>`, code, code, r.URL.Path)
}

func (f *fakeS3) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failCode != "" {
		f.writeErr(w, r, f.failStatus, f.failCode)
		return
	}
	prefix := "/" + f.bucket + "/"
	// 桶级请求（HEAD 桶 / GET ?location）：桶存在，直接 200
	if r.URL.Path == "/"+f.bucket || r.URL.Path == prefix {
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		}
		return
	}
	if !strings.HasPrefix(r.URL.Path, prefix) {
		f.writeErr(w, r, http.StatusNotFound, "NoSuchBucket")
		return
	}
	key := strings.TrimPrefix(r.URL.Path, prefix)

	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		// 明文 HTTP 下 minio-go 用 aws-chunked 流式签名上传，真实 S3 会解码，这里也要解码
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
			b = decodeAWSChunked(b)
		}
		f.objects[key] = b
		f.types[key] = r.Header.Get("Content-Type")
		w.Header().Set("ETag", `"fake"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.objects, key)
		f.deleted = append(f.deleted, key)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet, http.MethodHead:
		b, ok := f.objects[key]
		if !ok {
			f.writeErr(w, r, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Type", f.types[key])
		w.Header().Set("ETag", `"fake"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		if rg := r.Header.Get("Range"); rg != "" && r.Method == http.MethodGet {
			f.ranges = append(f.ranges, rg)
			var from, to int
			if _, err := fmt.Sscanf(rg, "bytes=%d-%d", &from, &to); err == nil && from < len(b) {
				if to >= len(b) {
					to = len(b) - 1
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, len(b)))
				w.Header().Set("Content-Length", fmt.Sprint(to-from+1))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(b[from : to+1])
				return
			}
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(b)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// specFake 返回指向假 S3 的配置：自定义 endpoint + path 寻址 + 明文 HTTP。
func (f *fakeS3) spec() Spec {
	return Spec{Provider: ProviderS3, Endpoint: "http://" + f.host(), Region: "us-east-1", Bucket: f.bucket,
		Addressing: AddressingPath, AccessKey: "ak", SecretKey: "sk"}
}

// decodeAWSChunked 解码 aws-chunked 请求体：每块是 "<十六进制长度>;chunk-signature=...\r\n<数据>\r\n"，长度为 0 的块结束。
func decodeAWSChunked(b []byte) []byte {
	var out []byte
	for len(b) > 0 {
		i := strings.Index(string(b), "\r\n")
		if i < 0 {
			break
		}
		var size int
		if _, err := fmt.Sscanf(strings.SplitN(string(b[:i]), ";", 2)[0], "%x", &size); err != nil || size == 0 {
			break
		}
		b = b[i+2:]
		if size > len(b) {
			size = len(b)
		}
		out = append(out, b[:size]...)
		b = b[size:]
		b = []byte(strings.TrimPrefix(string(b), "\r\n"))
	}
	return out
}
