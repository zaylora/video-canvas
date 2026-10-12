package plugin

import (
	"bytes"
	"encoding/json"
	"strings"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// 本文件：统一结果的简写（设计文档 docs/design/插件简化/插件简化.md §6.3）。
//
// AI 写插件时最容易写偏的是统一结果的精确结构，所以宿主在校验之前先把几种常见的“漏写”补全，
// 补完再走原有的严格校验。只放宽漏写，不放宽写错：
//   - outputs 的某一项是字符串：文本模型当正文，其余模型当 url 产物；
//   - outputs 的某一项是对象但缺 type：有 url 补 "url"，有 text 补 "text"；
//   - error 是非空字符串：当 message，class 为 terminal；
//   - error 是对象但缺 class（或 class 为空串）：class 补 terminal。
//
// 其余一律不动，交给原有校验报错：status 不是四个合法值之一、class 写了但不合法、
// 对象既没有 type 也没有 url / text、字符串不是 http(s) 地址（url 产物校验）等。
// status 尤其不放宽：“上游的哪个状态算成功”只有插件知道，宿主猜错会把失败当成功入库。

// normalizeResult 返回补全后的统一结果 JSON；结构不是对象或没有可补的内容时原样返回。
func (o *operation) normalizeResult(raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return raw
	}
	changed := false
	if outs, ok := obj["outputs"]; ok {
		if fixed, did := normalizeOutputs(outs, o.modelKind()); did {
			obj["outputs"], changed = fixed, true
		}
	}
	if e, ok := obj["error"]; ok {
		if fixed, did := normalizeError(e); did {
			obj["error"], changed = fixed, true
		}
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

// normalizeOutputs 把 outputs 数组里的字符串、缺 type 的对象补成完整的产物对象。
func normalizeOutputs(raw json.RawMessage, kind string) (json.RawMessage, bool) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw, false
	}
	changed := false
	for i, item := range items {
		fixed, did := normalizeOutput(item, kind)
		if did {
			items[i], changed = fixed, true
		}
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(items)
	if err != nil {
		return raw, false
	}
	return out, true
}

func normalizeOutput(raw json.RawMessage, kind string) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, false
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return raw, false
		}
		typ, field := provider.OutputURL, "url"
		if kind == modelcfg.KindText {
			typ, field = provider.OutputText, "text"
		}
		out, err := json.Marshal(map[string]string{"type": typ, field: s})
		if err != nil {
			return raw, false
		}
		return out, true
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return raw, false
		}
		if hasNonEmptyString(obj, "type") {
			return raw, false
		}
		switch {
		case hasNonEmptyString(obj, "url"):
			obj["type"] = json.RawMessage(`"url"`)
		case hasNonEmptyString(obj, "text"):
			obj["type"] = json.RawMessage(`"text"`)
		default:
			return raw, false
		}
		out, err := json.Marshal(obj)
		if err != nil {
			return raw, false
		}
		return out, true
	}
	return raw, false
}

// normalizeError 把字符串形式的 error 补成对象，把缺 class 的对象补上 terminal。
func normalizeError(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, false
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil || strings.TrimSpace(s) == "" {
			return raw, false // 空字符串没有任何信息，交给原校验报错
		}
		out, err := json.Marshal(map[string]string{"class": string(provider.ClassTerminal), "message": s})
		if err != nil {
			return raw, false
		}
		return out, true
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || len(obj) == 0 {
			return raw, false // 空对象没有任何信息，交给原校验报错
		}
		if cls, ok := obj["class"]; ok && !isNull(cls) && string(bytes.TrimSpace(cls)) != `""` {
			return raw, false // 写了 class：合法与否由原校验判断
		}
		obj["class"] = json.RawMessage(`"` + string(provider.ClassTerminal) + `"`)
		out, err := json.Marshal(obj)
		if err != nil {
			return raw, false
		}
		return out, true
	}
	return raw, false
}

func hasNonEmptyString(obj map[string]json.RawMessage, key string) bool {
	raw, ok := obj[key]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && s != ""
}
