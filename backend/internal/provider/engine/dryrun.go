//go:build legacy

package engine

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

// redactedSecret 是 DryRun 里代替凭证明文的占位符：DryRun 不解析凭证。
const redactedSecret = "***"

// DryRunResult 是干跑的结果：渲染出的上传与提交请求，不发送。
type DryRunResult struct {
	Uploads []DryRunRequest `json:"uploads,omitempty"` // 每个媒体字段一个上传请求（平台有 upload 操作时）
	Mapping any             `json:"mapping,omitempty"` // 渲染后的 model.mapping，方便核对
	Submit  DryRunRequest   `json:"submit"`
}

// DryRunRequest 是一个渲染好的请求（headers 已脱敏，body 是渲染后的内容）。
type DryRunRequest struct {
	Name    string            `json:"name"` // upload:<字段名> / submit
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`
}

// InputError 是干跑输入不符合 input_schema 时返回的错误，带字段级信息。
type InputError struct {
	Errors []dsl.FieldError
}

func (e *InputError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, f := range e.Errors {
		msgs = append(msgs, f.Message)
	}
	return "输入不合法：" + strings.Join(msgs, "；")
}

// DryRun 用示例输入渲染出上传与提交请求，但不发送任何请求。
//   - 媒体输入不读取真实素材：上传请求里的文件用占位文件名，没有 upload 操作时 files.<字段> 是占位 URL；
//   - 凭证不解析，鉴权位置用 *** 代替；
//   - 会校验 URL 的域名在 allowed_hosts 内（不做 DNS 解析），方便管理员在保存前发现 base_url 写错。
//
// input 不符合 input_schema 时返回 *InputError。
func DryRun(ctx context.Context, snap *dsl.Snapshot, input map[string]any) (*DryRunResult, error) {
	// 1. 快照与提交操作必须存在
	if snap == nil {
		return nil, fmt.Errorf("缺少配置快照")
	}
	p := &snap.Provider
	if p.Operations.Submit == nil {
		return nil, fmt.Errorf("平台没有配置提交操作")
	}

	// 2. 校验并规范化输入。干跑里媒体字段填任意合法的 asset id 即可，不会读取素材
	norm, ferrs := dsl.ValidateInput(snap.Model.InputSchema, input)
	if len(ferrs) > 0 {
		return nil, &InputError{Errors: ferrs}
	}

	e := &engine{opts: Options{Now: time.Now}}
	task := provider.TaskRef{}
	const webhook = "https://your-domain.example/api/v1/webhooks/{provider}/{token}"
	res := &DryRunResult{}

	// 3. 媒体字段：有 upload 操作就渲染上传请求，files 用占位引用；否则 files 用占位 URL
	files := map[string]any{}
	for _, name := range dsl.MediaFieldNames(snap.Model.InputSchema) {
		if _, ok := norm[name]; !ok {
			continue
		}
		if p.Operations.Upload == nil {
			files[name] = "https://placeholder.invalid/assets/" + name
			continue
		}
		field, _ := snap.Model.InputSchema.Get(name)
		fileName, mimeType := placeholderFile(name, field.Type)
		rc := e.baseContext(snap, norm, nil, task, webhook, nil)
		rc.Upload = map[string]any{"field": name, "kind": field.Type, "file_name": fileName, "mime_type": mimeType}
		req, err := dryRunRequest(p, p.Operations.Upload, rc, "upload:"+name)
		if err != nil {
			return nil, fmt.Errorf("渲染 upload:%s 失败：%w", name, err)
		}
		if m, ok := req.Body.(map[string]any); ok {
			m[p.Operations.Upload.Encoding.FileField] = fmt.Sprintf("<文件 %s，%s>", fileName, mimeType)
		} else if p.Operations.Upload.Encoding.FileField != "" {
			req.Body = map[string]any{p.Operations.Upload.Encoding.FileField: fmt.Sprintf("<文件 %s，%s>", fileName, mimeType)}
		}
		res.Uploads = append(res.Uploads, *req)
		files[name] = "<上传后的文件引用:" + name + ">"
	}

	// 4. mapping 与 submit 请求
	rc := e.baseContext(snap, norm, files, task, webhook, nil)
	mapping, err := dsl.RenderValue(snap.Model.Mapping, rc)
	if err != nil {
		return nil, fmt.Errorf("渲染 mapping 失败：%w", err)
	}
	rc.Model["mapping"] = mapping
	res.Mapping = mapping
	req, err := dryRunRequest(p, p.Operations.Submit, rc, "submit")
	if err != nil {
		return nil, fmt.Errorf("渲染 submit 失败：%w", err)
	}
	res.Submit = *req
	return res, nil
}

// dryRunRequest 渲染一个请求：加上占位鉴权，校验域名白名单，输出脱敏后的结果。
func dryRunRequest(p *dsl.ProviderConfig, op *dsl.Operation, rc *dsl.RenderContext, name string) (*DryRunRequest, error) {
	b, err := buildRequest(p, op, rc)
	if err != nil {
		return nil, err
	}
	if err := applyAuth(b, p.Auth, redactedSecret); err != nil {
		return nil, err
	}
	if err := checkURLAllowed(p.AllowedHosts, b.URL.Scheme, b.URL.Hostname(), b.URL.User != nil); err != nil {
		return nil, err
	}
	headers := map[string]string{}
	for k, v := range b.Headers {
		headers[k] = v
	}
	switch {
	case b.Multipart:
		headers["Content-Type"] = "multipart/form-data"
	case b.Body != nil:
		headers["Content-Type"] = "application/json"
	}
	var body any
	if m, ok := b.Body.(map[string]any); ok && b.Multipart {
		cp := make(map[string]any, len(m)+1)
		for k, v := range m {
			cp[k] = v
		}
		body = cp
	} else {
		body = b.Body
	}
	return &DryRunRequest{Name: name, Method: b.Method, URL: displayURL(b.URL), Headers: headers, Body: body}, nil
}

// displayURL 输出 URL 文本；query 鉴权的占位符 *** 不做百分号转义，方便阅读。
func displayURL(u *url.URL) string {
	return strings.ReplaceAll(u.String(), "%2A%2A%2A", redactedSecret)
}

// placeholderFile 生成干跑用的占位文件名与 MIME 类型。
func placeholderFile(field, kind string) (name, mime string) {
	switch kind {
	case dsl.FieldImage:
		return field + "-placeholder.png", "image/png"
	case dsl.FieldVideo:
		return field + "-placeholder.mp4", "video/mp4"
	case dsl.FieldAudio:
		return field + "-placeholder.mp3", "audio/mpeg"
	}
	return field + "-placeholder.bin", "application/octet-stream"
}
