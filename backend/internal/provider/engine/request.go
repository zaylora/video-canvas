//go:build legacy

package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"sort"
	"strings"

	"video-canvas/internal/provider/dsl"
)

// builtRequest 是渲染完成、尚未加鉴权的请求。DryRun 和真实发送共用它，保证“所见即所发”。
type builtRequest struct {
	Method  string
	URL     *url.URL          // 含渲染出来的查询参数
	Headers map[string]string // 渲染出来的请求头（不含鉴权与 Content-Type）
	Body    any               // json：任意 JSON；multipart：map[string]any（表单字段）
	// Multipart 为 true 时 Body 是表单字段；上传操作还会附带一个文件。
	Multipart bool
	FileField string
}

// buildRequest 按操作声明渲染出请求：path / headers / query / body。
// path 里 ${ } 表达式的结果会做路径转义，防止用户输入改变请求目标；最终 URL 必须仍然落在 base_url 的域名下。
func buildRequest(p *dsl.ProviderConfig, op *dsl.Operation, rc *dsl.RenderContext) (*builtRequest, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("base_url 不合法")
	}
	if !strings.HasPrefix(op.Path, "/") {
		return nil, fmt.Errorf("path 必须以 / 开头")
	}
	path, err := dsl.RenderStringEscaped(op.Path, rc, url.PathEscape)
	if err != nil {
		return nil, fmt.Errorf("渲染 path 失败：%w", err)
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, fmt.Errorf("渲染后的 path 不合法：%q", path)
	}
	u, err := url.Parse(strings.TrimRight(base.String(), "/") + path)
	if err != nil {
		return nil, fmt.Errorf("拼接 URL 失败：%w", err)
	}
	// 拼出来的 URL 必须还是 base_url 的主机：防止 path 里的 @ 等字符把主机换掉
	if u.Host != base.Host || u.Scheme != base.Scheme || u.User != nil {
		return nil, fmt.Errorf("渲染后的 URL 偏离了 base_url")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == ".." {
			return nil, fmt.Errorf("path 不允许包含 ..")
		}
	}

	q := u.Query()
	for _, k := range sortedKeys(op.Query) {
		v, err := dsl.RenderValue(op.Query[k], rc)
		if err != nil {
			return nil, fmt.Errorf("渲染 query.%s 失败：%w", k, err)
		}
		if v == nil { // 整体渲染成 nil 的参数不发送
			continue
		}
		q.Set(k, dsl.Stringify(v))
	}
	u.RawQuery = q.Encode()

	headers := map[string]string{}
	for _, k := range sortedKeys(op.Headers) {
		v, err := dsl.RenderValue(op.Headers[k], rc)
		if err != nil {
			return nil, fmt.Errorf("渲染 headers.%s 失败：%w", k, err)
		}
		s := dsl.Stringify(v)
		if strings.ContainsAny(s, "\r\n") {
			return nil, fmt.Errorf("headers.%s 的值包含换行符", k)
		}
		headers[k] = s
	}

	b := &builtRequest{Method: op.Method, URL: u, Headers: headers}
	if op.Body != nil {
		body, err := dsl.RenderValue(op.Body, rc)
		if err != nil {
			return nil, fmt.Errorf("渲染 body 失败：%w", err)
		}
		b.Body = body
	}
	if op.Encoding.Type == dsl.EncodingMultipart {
		b.Multipart = true
		b.FileField = op.Encoding.FileField
		if b.Body != nil {
			if _, ok := b.Body.(map[string]any); !ok {
				return nil, fmt.Errorf("multipart 的 body 必须是对象")
			}
		}
	}
	return b, nil
}

// applyAuth 按 auth 配置把凭证加进请求。secret 是明文（真实发送）或 "***"（DryRun）。
// 凭证只在这里出现，不进表达式上下文。
func applyAuth(b *builtRequest, a dsl.AuthConfig, secret string) error {
	switch a.Type {
	case dsl.AuthNone, "":
		return nil
	case dsl.AuthBearer:
		b.Headers["Authorization"] = "Bearer " + secret
	case dsl.AuthHeader:
		b.Headers[a.Name] = secret
	case dsl.AuthQuery:
		q := b.URL.Query()
		q.Set(a.Name, secret)
		b.URL.RawQuery = q.Encode()
	case dsl.AuthBodyField:
		switch body := b.Body.(type) {
		case nil:
			b.Body = map[string]any{a.Name: secret}
		case map[string]any:
			cp := make(map[string]any, len(body)+1)
			for k, v := range body {
				cp[k] = v
			}
			cp[a.Name] = secret
			b.Body = cp
		default:
			return fmt.Errorf("body_field 鉴权要求请求体是对象")
		}
	default:
		return fmt.Errorf("不支持的鉴权类型 %q", a.Type)
	}
	return nil
}

// uploadPart 是上传操作附带的文件。
type uploadPart struct {
	FileName string
	MimeType string
	Body     io.Reader
}

// encodedBody 是编码好的请求体。
type encodedBody struct {
	reader      io.Reader
	contentType string
	length      int64  // 未知为 -1
	preview     string // 给 Trace 用的文字描述（不含二进制内容）
}

// encodeBody 把渲染好的 body 编码成 JSON 或 multipart。multipart 带文件时用管道流式写入，避免把大视频读进内存。
func encodeBody(b *builtRequest, file *uploadPart) (*encodedBody, error) {
	if b.Multipart {
		return encodeMultipart(b, file)
	}
	if b.Body == nil {
		return &encodedBody{length: 0}, nil
	}
	raw, err := json.Marshal(b.Body)
	if err != nil {
		return nil, fmt.Errorf("编码 JSON 请求体失败：%w", err)
	}
	return &encodedBody{reader: bytes.NewReader(raw), contentType: "application/json", length: int64(len(raw)), preview: string(raw)}, nil
}

func encodeMultipart(b *builtRequest, file *uploadPart) (*encodedBody, error) {
	fields, _ := b.Body.(map[string]any)
	names := sortedKeys(fields)

	preview := &strings.Builder{}
	for _, k := range names {
		fmt.Fprintf(preview, "%s=%s\n", k, formValue(fields[k]))
	}
	if file != nil {
		fmt.Fprintf(preview, "%s=<文件 %s，%s，二进制内容不记录>\n", b.FileField, file.FileName, file.MimeType)
	}

	writeFields := func(mw *multipart.Writer) error {
		for _, k := range names {
			if err := mw.WriteField(k, formValue(fields[k])); err != nil {
				return err
			}
		}
		return nil
	}

	// 没有文件：直接缓冲，长度已知
	if file == nil {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if err := writeFields(mw); err != nil {
			return nil, err
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}
		return &encodedBody{reader: &buf, contentType: mw.FormDataContentType(), length: int64(buf.Len()), preview: preview.String()}, nil
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := writeFields(mw)
		if err == nil {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(b.FileField), escapeQuotes(file.FileName)))
			mt := file.MimeType
			if mt == "" {
				mt = "application/octet-stream"
			}
			h.Set("Content-Type", mt)
			var part io.Writer
			if part, err = mw.CreatePart(h); err == nil {
				_, err = io.Copy(part, file.Body)
			}
		}
		if err == nil {
			err = mw.Close()
		}
		// 出错时让读取端（HTTP 传输层）也拿到错误；请求被取消时读取端会先关闭管道，这里的写入随之失败退出
		_ = pw.CloseWithError(err)
	}()
	return &encodedBody{reader: pr, contentType: mw.FormDataContentType(), length: -1, preview: preview.String()}, nil
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "", "\n", "")

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }

// formValue 把表单字段值转成文本：标量用 Stringify，对象 / 数组用 JSON。
func formValue(v any) string { return dsl.Stringify(v) }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
