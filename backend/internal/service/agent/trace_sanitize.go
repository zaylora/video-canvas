package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// 轨迹原文的整理：脱敏（图片换成引用）、截断（单行不超过上限）。都是纯函数，记录器和测试共用。

// dataURLPattern 匹配内联的 base64 data URL（图片、音频等），长度够长才算，免得误伤正文里提到的 "data:" 字样。
var dataURLPattern = regexp.MustCompile(`data:([a-zA-Z0-9.+/-]+);base64,[A-Za-z0-9+/=_-]{64,}`)

// maxUnparsedRequestBytes 是请求体不是合法 JSON 时，作为文本留下的最大字节数。
const maxUnparsedRequestBytes = 4096

// SanitizeChatRequest 把发给模型的请求体整理成可以进轨迹的形式：
//   - 请求头不在这里（调用方根本没把它交进来），渠道 Key 不可能出现；
//   - 工具声明全文只在每个片段的第一行（system 行）记录一次，请求里的 tools 换成 toolsHash 引用；
//   - 图片（data URL 或 URL）一律换成 {"asset_id": 指纹, ...}：指纹是内容的 sha256 前 16 位，原图和签名地址都不进轨迹。
//
// 请求体不是合法 JSON 时返回一个只含前 4KB 文本的对象，轨迹不能因为格式意外就丢掉线索。
func SanitizeChatRequest(body []byte, toolsHash string) any {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // 数字原样保留，不被转成 float64 丢精度
	var req map[string]any
	if err := dec.Decode(&req); err != nil || req == nil {
		text := string(body)
		if len(text) > maxUnparsedRequestBytes {
			text = text[:maxUnparsedRequestBytes]
		}
		return map[string]any{"unparsed": strings.ToValidUTF8(text, "")}
	}
	if t, ok := req["tools"].([]any); ok && len(t) > 0 {
		req["tools"] = toolsHash
	}
	for k, v := range req {
		req[k] = scrubImages(v)
	}
	return req
}

// scrubImages 递归地把图片部件换成引用，并抹掉字符串里内联的 data URL。
func scrubImages(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if ref, ok := imagePartRef(x); ok {
			return ref
		}
		for k, e := range x {
			x[k] = scrubImages(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = scrubImages(e)
		}
		return x
	case string:
		return dataURLPattern.ReplaceAllStringFunc(x, func(m string) string {
			ref := imageRef(m)
			return "[图片 " + ref["asset_id"].(string) + "，已从轨迹移除]"
		})
	}
	return v
}

// imagePartRef 识别 OpenAI / Anthropic 风格的图片部件并返回引用。
func imagePartRef(m map[string]any) (map[string]any, bool) {
	switch m["type"] {
	case "image_url", "input_image":
		switch u := m["image_url"].(type) {
		case map[string]any:
			if s, ok := u["url"].(string); ok {
				return imageRef(s), true
			}
		case string:
			return imageRef(u), true
		}
	case "image":
		if src, ok := m["source"].(map[string]any); ok {
			if s, ok := src["data"].(string); ok {
				return imageRef(s), true
			}
			if s, ok := src["url"].(string); ok {
				return imageRef(s), true
			}
		}
	}
	return nil, false
}

// imageRef 把一张图（data URL、base64 或 URL）换成稳定的引用：同一张图在不同请求里指纹相同，方便看出是不是同一张。
func imageRef(src string) map[string]any {
	sum := sha256.Sum256([]byte(src))
	ref := map[string]any{"asset_id": "img_" + hex.EncodeToString(sum[:8]), "bytes": len(src)}
	if rest, ok := strings.CutPrefix(src, "data:"); ok {
		if mime, _, found := strings.Cut(rest, ";"); found {
			ref["mime"] = mime
		}
		ref["source"] = "data"
	} else {
		ref["source"] = "url"
	}
	return ref
}

// fitLine 把一行原文序列化成不超过 limit 字节的 JSON，必要时逐步缩小，返回字节和有没有被截断。
// 顺序：先把超长字符串收成头部加标记（先 16KB、再 4KB、1KB、256 字节），还不够就丢掉 request.messages 中间的消息，
// 最后只留标识字段。截断过的行带 "truncated": true。line 会被就地修改。
func fitLine(line map[string]any, limit int) ([]byte, bool) {
	b := marshalLine(line)
	if len(b) <= limit {
		return b, false
	}
	line["truncated"] = true
	for _, n := range []int{16 << 10, 4 << 10, 1 << 10, 256} {
		line = shrinkStrings(line, n).(map[string]any)
		if b = marshalLine(line); len(b) <= limit {
			return b, true
		}
	}
	if req, ok := line["request"].(map[string]any); ok {
		if msgs, ok := req["messages"].([]any); ok && len(msgs) > 2 {
			// 保留第一条（通常是用户的任务）和最后一条，从第二条开始往后丢，直到放得下
			for len(msgs) > 2 {
				msgs = append(msgs[:1], msgs[2:]...)
				req["messages"], req["omitted_messages"] = msgs, intOf(req["omitted_messages"])+1
				if b = marshalLine(line); len(b) <= limit {
					return b, true
				}
			}
		}
	}
	// 实在放不下：只留标识，说明原文被丢弃
	stub := map[string]any{"truncated": true, "note": "原文超过单个 span 的上限，已丢弃"}
	for _, k := range []string{"span_id", "kind", "seq", "tool_call_id", "parent_tool_call_id", "phase", "segment_seq"} {
		if v, ok := line[k]; ok {
			stub[k] = v
		}
	}
	return marshalLine(stub), true
}

// marshalLine 序列化一行；map 里只有 JSON 能表示的值，序列化不会失败，万一失败返回空对象，调用方照样能写下去。
func marshalLine(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// shrinkStrings 递归地把长度超过 n 字节的字符串收成「头部 + 标记」。
func shrinkStrings(v any, n int) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = shrinkStrings(e, n)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = shrinkStrings(e, n)
		}
		return x
	case []map[string]any:
		for i, e := range x {
			x[i] = shrinkStrings(e, n).(map[string]any)
		}
		return x
	case string:
		return clipBytes(x, n)
	}
	return v
}

// clipBytes 把字符串截到不超过 n 字节（不切断汉字），超出时写明丢了多少字节。
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…[已截断 " + strconv.Itoa(len(s)-cut) + " 字节]"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func intOf(v any) int {
	if n, ok := v.(int); ok {
		return n
	}
	return 0
}
