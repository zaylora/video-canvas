package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
)

// MaxModelRequestBytes 是单次模型请求体完整留存的上限（脱敏后、压缩前）；超过的只留摘要。
// 一次对话调用带着完整上下文，正常在几十到几百 KB；上限放到 8MB 覆盖长会话，再大的不值得占库。
const MaxModelRequestBytes = 8 << 20

// ModelRequestRecorder 留存收紧后的 OpenAI 形状请求体，供事后分析"模型当时看到了什么"。
// 它是观测数据：实现出错时调用方只记日志，不能影响用户的运行。
type ModelRequestRecorder interface {
	// SaveModelCallBody 保存一条请求记录；同一次调用重复保存时不覆盖，也不是错误。
	SaveModelCallBody(ctx context.Context, b *model.AgentModelCallBody) error
}

// SetModelRequestRecorder 绑定模型请求记录器。装配阶段调用，运行开始前完成，之后不再改；不绑定就不记录。
func (b *AgentBridge) SetModelRequestRecorder(r ModelRequestRecorder) { b.reqRec = r }

// mediaRedactKeys 是多模态分段里会带出图片地址或内容的字段：地址可能是带签名的对象存储链接，内容是 base64，都不能留。
var mediaRedactKeys = map[string]bool{"url": true, "data": true, "file_data": true, "file_id": true}

// BuildModelRequestRecord 把 SanitizeRaw 后的 OpenAI 形状请求体做成可落库的记录：
// 摘要（消息数、工具数、除消息和工具外的顶层字段，及非空的 apiFormat）+ 脱敏后 gzip 压缩的完整内容。
// 图片等非文本分段里的地址和数据一律换成占位文字（导出边界：图片地址不出库），其余内容原文保留，渠道 Key 本来就不在请求体里。
// 超过 MaxModelRequestBytes 只留摘要；不是 JSON 对象返回 nil。
func BuildModelRequestRecord(callID, runID uint64, body []byte, apiFormat string) *model.AgentModelCallBody {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil || top == nil {
		return nil
	}
	rec := &model.AgentModelCallBody{CallID: callID, RunID: runID}
	params := map[string]json.RawMessage{}
	for k, v := range top {
		if k != "messages" && k != "tools" {
			params[k] = v
		}
	}
	if apiFormat != "" {
		params["api_format"], _ = json.Marshal(apiFormat) // 字符串序列化不会失败；空格式不加键，保持老摘要形状。
	}
	if len(params) > 0 {
		rec.Params, _ = json.Marshal(params) // 全是已经解析成功的 RawMessage，序列化不会失败
	}
	var tools []json.RawMessage
	_ = json.Unmarshal(top["tools"], &tools)
	rec.ToolCount = len(tools)

	var messages []any
	dec := json.NewDecoder(bytes.NewReader(top["messages"]))
	dec.UseNumber() // 数字原样回写，不被转成浮点
	_ = dec.Decode(&messages)
	rec.MessageCount = len(messages)
	for _, m := range messages {
		redactMessage(m)
	}

	out := make(map[string]any, len(top))
	for k, v := range top {
		out[k] = v
	}
	if top["messages"] != nil {
		out["messages"] = messages
	}
	data, err := marshalNoEscape(out)
	if err != nil {
		return rec
	}
	rec.RawBytes = len(data)
	if rec.RawBytes > MaxModelRequestBytes {
		return rec
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil || zw.Close() != nil {
		return rec
	}
	rec.Body = buf.Bytes()
	return rec
}

// redactMessage 把一条消息里非文本分段的地址和数据换成占位文字，文本分段和纯文本消息不动。
func redactMessage(m any) {
	msg, ok := m.(map[string]any)
	if !ok {
		return
	}
	parts, ok := msg["content"].([]any)
	if !ok {
		return
	}
	for _, p := range parts {
		part, ok := p.(map[string]any)
		if !ok || part["type"] == "text" {
			continue
		}
		redactMedia(part)
	}
}

// redactMedia 递归地把分段里的 url / data 等字段换成「已省略 N 字符」，保留长度方便判断是哪一类内容。
func redactMedia(v map[string]any) {
	for k, val := range v {
		switch t := val.(type) {
		case string:
			if mediaRedactKeys[k] {
				v[k] = fmt.Sprintf("[已省略 %d 字符]", len([]rune(t)))
			}
		case map[string]any:
			redactMedia(t)
		}
	}
}

// marshalNoEscape 序列化时不把 < > & 转成 < 这类转义：留存下来的内容是给人和 AI 读的。
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// recordModelRequest 保存一次模型调用的请求体。失败只记日志：观测数据丢一条，不该让用户的对话失败。
// 用不会被取消的上下文：用户停止运行时请求的上下文已取消，这次调用照样要留下记录。
func (b *AgentBridge) recordModelRequest(ctx context.Context, runID, callID uint64, sent []byte, apiFormat string) {
	rec := BuildModelRequestRecord(callID, runID, sent, apiFormat)
	if rec == nil {
		return
	}
	if purpose := ModelPurposeFrom(ctx); purpose != "" {
		var params map[string]any
		_ = json.Unmarshal(rec.Params, &params)
		params["purpose"] = purpose
		rec.Params, _ = json.Marshal(params)
	}
	rec.CreatedAt = b.d.Now()
	if err := b.reqRec.SaveModelCallBody(context.WithoutCancel(ctx), rec); err != nil {
		logger.Warn("记录模型请求失败", zap.Error(err), zap.Uint64("run_id", runID), zap.Uint64("call_id", callID))
	}
}
