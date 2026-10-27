package llmgateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// geminiEndpoint 保留渠道路径；未配置 antigravity 路径段时补上中转需要的前缀。
func geminiEndpoint(base *url.URL, model string) string {
	prefix := strings.TrimRight(base.String(), "/")
	if !strings.Contains(base.Path+"/", "/antigravity/") {
		prefix += "/antigravity"
	}
	return prefix + "/v1beta/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse"
}

type geminiContent struct {
	Role  string           `json:"role,omitempty"`
	Parts []map[string]any `json:"parts"`
}

type openAIMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type openAIRequest struct {
	Messages []openAIMessage `json:"messages"`
	Tools    []struct {
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
	ToolChoice  json.RawMessage `json:"tool_choice"`
	MaxTokens   json.RawMessage `json:"max_tokens"`
	Temperature json.RawMessage `json:"temperature"`
	TopP        json.RawMessage `json:"top_p"`
	Stop        json.RawMessage `json:"stop"`
}

// ToGemini 把收紧后的 OpenAI 请求翻译成 Gemini 原生请求；错误不包含消息或媒体地址。
func ToGemini(openaiBody []byte) ([]byte, error) {
	var in openAIRequest
	if json.Unmarshal(openaiBody, &in) != nil || len(in.Messages) == 0 {
		return nil, fmt.Errorf("%w: messages 必须是非空消息数组", ErrInvalidRequest)
	}
	out, err := geminiMessages(in.Messages)
	if err != nil {
		return nil, err
	}
	if len(in.Tools) > 0 {
		out["tools"] = in.geminiTools()
	}
	choice, err := geminiToolChoice(in.ToolChoice)
	if err != nil {
		return nil, err
	}
	if choice != nil {
		out["toolConfig"] = map[string]any{"functionCallingConfig": choice}
	}
	config, err := in.geminiGenerationConfig()
	if err != nil {
		return nil, err
	}
	if len(config) > 0 {
		out["generationConfig"] = config
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("%w: 工具定义格式不正确", ErrInvalidRequest)
	}
	return body, nil
}

func geminiMessages(messages []openAIMessage) (map[string]any, error) {
	var system []map[string]any
	contents := make([]geminiContent, 0, len(messages))
	names := make(map[string]string)
	previousRole := ""
	for _, msg := range messages {
		role, parts, err := msg.geminiParts(names, previousRole == "tool")
		if err != nil {
			return nil, err
		}
		previousRole = msg.Role
		if role == "system" {
			system = append(system, parts...)
			continue
		}
		if len(parts) == 0 {
			continue
		}
		if n := len(contents); n > 0 && contents[n-1].Role == role {
			contents[n-1].Parts = append(contents[n-1].Parts, parts...)
		} else {
			contents = append(contents, geminiContent{Role: role, Parts: parts})
		}
	}
	out := map[string]any{"contents": contents}
	if len(system) > 0 {
		out["systemInstruction"] = geminiContent{Parts: system}
	}
	return out, nil
}

func (msg openAIMessage) geminiParts(names map[string]string, afterTool bool) (string, []map[string]any, error) {
	if msg.Role == "tool" {
		name := names[msg.ToolCallID]
		if name == "" {
			return "", nil, fmt.Errorf("%w: 工具结果找不到对应的调用，请重新发起对话", ErrInvalidRequest)
		}
		var text string
		if json.Unmarshal(msg.Content, &text) != nil {
			return "", nil, fmt.Errorf("%w: 工具结果必须是文字", ErrInvalidRequest)
		}
		return "user", []map[string]any{{"functionResponse": map[string]any{"name": name, "response": map[string]any{"content": text}}}}, nil
	}
	parts, err := geminiMessageParts(msg.Content, msg.Role == "user" && afterTool)
	if err != nil {
		return "", nil, err
	}
	switch msg.Role {
	case "system", "user":
		return msg.Role, parts, nil
	case "assistant":
		calls, err := msg.geminiCalls(names)
		return "model", append(parts, calls...), err
	default:
		return "", nil, fmt.Errorf("%w: 不支持的消息角色", ErrInvalidRequest)
	}
}

func (msg openAIMessage) geminiCalls(names map[string]string) ([]map[string]any, error) {
	parts := make([]map[string]any, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		args := map[string]json.RawMessage{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
				return nil, fmt.Errorf("%w: 工具调用参数必须是 JSON 对象", ErrInvalidRequest)
			}
		}
		part := map[string]any{"functionCall": map[string]any{"name": call.Function.Name, "args": args}}
		if sig := decodeCallID(call.ID); sig != "" {
			part["thoughtSignature"] = sig
		}
		parts = append(parts, part)
		names[call.ID] = call.Function.Name
	}
	return parts, nil
}

func (in openAIRequest) geminiTools() []any {
	decls := make([]map[string]any, 0, len(in.Tools))
	for _, tool := range in.Tools {
		fn := tool.Function
		params := fn.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		decls = append(decls, map[string]any{"name": fn.Name, "description": fn.Description, "parameters": CleanSchemaForGemini(params)})
	}
	return []any{map[string]any{"functionDeclarations": decls}}
}

func geminiToolChoice(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var choice string
	if json.Unmarshal(raw, &choice) == nil {
		if choice == "none" {
			return map[string]any{"mode": "NONE"}, nil
		}
		return nil, nil
	}
	var selected struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &selected) != nil || selected.Function.Name == "" {
		return nil, fmt.Errorf("%w: 工具选择格式不正确", ErrInvalidRequest)
	}
	return map[string]any{"mode": "ANY", "allowedFunctionNames": []string{selected.Function.Name}}, nil
}

func (in openAIRequest) geminiGenerationConfig() (map[string]any, error) {
	config := make(map[string]any)
	for key, value := range map[string]json.RawMessage{"maxOutputTokens": in.MaxTokens, "temperature": in.Temperature, "topP": in.TopP} {
		if len(value) > 0 {
			config[key] = value
		}
	}
	if len(in.Stop) == 0 || bytes.Equal(bytes.TrimSpace(in.Stop), []byte("null")) {
		return config, nil
	}
	var stop string
	if json.Unmarshal(in.Stop, &stop) == nil {
		config["stopSequences"] = []string{stop}
	} else {
		var stops []string
		if json.Unmarshal(in.Stop, &stops) != nil {
			return nil, fmt.Errorf("%w: 停止词必须是文字或文字数组", ErrInvalidRequest)
		}
		config["stopSequences"] = stops
	}
	return config, nil
}

// geminiMessageParts 保留文本和图片顺序；工具附图的说明文字不是工具结果的一部分。
func geminiMessageParts(content json.RawMessage, toolImages bool) ([]map[string]any, error) {
	if len(content) == 0 || bytes.Equal(bytes.TrimSpace(content), []byte("null")) {
		return nil, nil
	}
	var text string
	if json.Unmarshal(content, &text) == nil {
		if text == "" {
			return nil, nil
		}
		return []map[string]any{{"text": text}}, nil
	}
	var in []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
		Video *Video `json:"video"`
	}
	if json.Unmarshal(content, &in) != nil {
		return nil, fmt.Errorf("%w: 消息内容必须是文字或分段数组", ErrInvalidRequest)
	}
	parts := make([]map[string]any, 0, len(in))
	for i, p := range in {
		switch p.Type {
		case "text":
			if toolImages && i == 0 && p.Text == "Attached image(s) from tool result:" {
				continue
			}
			parts = append(parts, map[string]any{"text": p.Text})
		case "image_url":
			part, err := geminiImagePart(p.ImageURL.URL)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case "video":
			part, err := geminiVideoPart(p.Video)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		default:
			return nil, fmt.Errorf("%w: 不支持的消息分段", ErrInvalidRequest)
		}
	}
	return parts, nil
}

// geminiVideoPart 只序列化一个有效的视频输入，错误信息不回显签名地址或视频内容。
func geminiVideoPart(v *Video) (map[string]any, error) {
	if v == nil || !strings.HasPrefix(v.MimeType, "video/") || (v.URL == "") == (v.Data == "") {
		return nil, fmt.Errorf("%w: 视频需要 MIME 类型及地址或 Base64 数据", ErrInvalidRequest)
	}
	if v.URL != "" {
		u, err := url.Parse(v.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return nil, fmt.Errorf("%w: 视频地址必须是 HTTPS", ErrInvalidRequest)
		}
		return map[string]any{"fileData": map[string]any{"mimeType": v.MimeType, "fileUri": v.URL}}, nil
	}
	if _, err := base64.StdEncoding.DecodeString(v.Data); err != nil {
		return nil, fmt.Errorf("%w: 视频 Base64 数据格式不正确", ErrInvalidRequest)
	}
	return map[string]any{"inlineData": map[string]any{"mimeType": v.MimeType, "data": v.Data}}, nil
}

func geminiImagePart(address string) (map[string]any, error) {
	if strings.HasPrefix(address, "data:") {
		header, data, ok := strings.Cut(strings.TrimPrefix(address, "data:"), ",")
		mime, encoded := strings.CutSuffix(header, ";base64")
		if !ok || !encoded || mime == "" {
			return nil, fmt.Errorf("%w: 图片 data URL 格式不正确", ErrInvalidRequest)
		}
		return map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": data}}, nil
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: 图片必须是 https 地址或 base64 data URL", ErrInvalidRequest)
	}
	mime := "image/jpeg"
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".png":
		mime = "image/png"
	case ".webp":
		mime = "image/webp"
	case ".gif":
		mime = "image/gif"
	case ".heic":
		mime = "image/heic"
	case ".heif":
		mime = "image/heif"
	}
	return map[string]any{"fileData": map[string]any{"mimeType": mime, "fileUri": address}}, nil
}

// decodeCallID 只解码网关生成的 id，其他来源或损坏的 id 不携带 Gemini 签名。
func decodeCallID(id string) string {
	if len(id) <= 12 || !strings.HasPrefix(id, "gc_") || id[11] != '_' {
		return ""
	}
	for _, c := range id[3:11] {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return ""
		}
	}
	b, err := base64.RawURLEncoding.DecodeString(id[12:])
	if err != nil {
		return ""
	}
	return string(b)
}
