// 本文件：用户输入校验：按模型能力（capabilities）检查并规范化用户提交的任务输入，一次返回全部字段级错误；
// 另提供参考素材的提取（MediaRefs）。

package modelcfg

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxSafeAssetID 是 float64 能精确表示的最大整数（2^53）；更大的 asset id 必须以字符串或 json.Number 传入。
const maxSafeAssetID = 1 << 53

// refKindsOfOp 返回生成方式允许引用的素材种类：文生不引用素材；图生只引用图片；全能参考不限类型（以 refs 里开启的为准）。
func refKindsOfOp(op string) []string {
	switch op {
	case OpI2V, OpI2I:
		return []string{"image"}
	case OpOmni:
		return []string{"image", "video", "audio"}
	}
	return nil
}

// validateInput 按能力校验并规范化任务输入。输出只包含这些键：
//   - prompt：string，必填，不超过 prompt.max_length 个字符；
//   - op：string，仅 video / image，省略取第一种生成方式；
//   - 每个生成参数：只接受 open=true 的，其余一律取 default（enum -> 选项里声明的值，数字统一为 float64；number -> float64；boolean -> bool）；
//   - images / videos / audios：素材 id（uint64）数组，只保留当前生成方式允许、且 refs 里开启的种类，数量不超过 refs.<种类>.max。
//
// 未知键忽略。
func validateInput(kind string, c Capabilities, in map[string]any) (map[string]any, []FieldError) {
	out := map[string]any{}
	var errs []FieldError

	errs = append(errs, inputPrompt(c, in, out)...)
	op, opErrs := inputOp(kind, c, in, out)
	errs = append(errs, opErrs...)
	errs = append(errs, inputParams(c, in, out)...)
	errs = append(errs, inputRefs(c, op, in, out)...)

	if len(errs) > 0 {
		return nil, errs
	}
	return out, nil
}

// inputPrompt 校验提示词：必填、不超过字数上限。
func inputPrompt(c Capabilities, in, out map[string]any) []FieldError {
	prompt, _ := in["prompt"].(string)
	switch n := utf8.RuneCountInString(prompt); {
	case strings.TrimSpace(prompt) == "":
		return []FieldError{{Field: "prompt", Message: "提示词不能为空"}}
	case n > c.Prompt.MaxLength:
		return []FieldError{{Field: "prompt", Message: fmt.Sprintf("提示词不能超过 %d 个字符（当前 %d 个）", c.Prompt.MaxLength, n)}}
	}
	out["prompt"] = prompt
	return nil
}

// inputOp 校验生成方式（仅 video / image）：省略取第一种；返回最终生效的方式。
func inputOp(kind string, c Capabilities, in, out map[string]any) (string, []FieldError) {
	if len(opsOfKind[kind]) == 0 {
		return "", nil
	}
	op := c.firstOp()
	if raw, ok := in["op"].(string); ok && raw != "" {
		if !inStrings(c.Ops, raw) {
			return op, []FieldError{{Field: "op", Message: "生成方式必须是 " + strings.Join(c.Ops, " / ") + " 之一"}}
		}
		op = raw
	}
	if op != "" {
		out["op"] = op
	}
	return op, nil
}

// inputParams 校验生成参数：只接受开放的，没传或空白取默认值。
func inputParams(c Capabilities, in, out map[string]any) []FieldError {
	var errs []FieldError
	for _, e := range c.Params {
		var raw any
		if e.Open {
			raw = in[e.Name]
		}
		if raw == nil || isBlank(raw) {
			raw = e.Default
		}
		if raw == nil {
			if e.Type == ParamBoolean {
				out[e.Name] = false
			}
			continue
		}
		v, msg := normalizeParam(e.ParamField, raw)
		if msg != "" {
			errs = append(errs, FieldError{Field: e.Name, Message: e.label(e.Name) + " " + msg})
			continue
		}
		out[e.Name] = v
	}
	return errs
}

// inputRefs 校验参考素材：按生成方式与 refs 开关过滤，检查数量；不要求一定有素材（上游节点可能还没出图）。
func inputRefs(c Capabilities, op string, in, out map[string]any) []FieldError {
	var errs []FieldError
	allowed := refKindsOfOp(op)
	for _, m := range MediaKinds {
		spec := c.Refs.Of(m.Kind)
		if in[m.Key] == nil || !spec.On || !inStrings(allowed, m.Kind) {
			continue // 当前方式不使用这种素材：忽略
		}
		ids, err := refIDs(in[m.Key])
		switch {
		case err != "":
			errs = append(errs, FieldError{Field: m.Key, Message: err})
		case len(ids) > spec.Max:
			errs = append(errs, FieldError{Field: m.Key, Message: fmt.Sprintf("最多 %d 个%s素材（当前 %d 个）", spec.Max, kindLabel(m.Kind), len(ids))})
		case len(ids) > 0:
			out[m.Key] = ids
		}
	}
	return errs
}

// refIDs 把一个素材数组规范成 uint64 id 列表；格式不对返回中文错误。
func refIDs(raw any) ([]uint64, string) {
	var list []any
	switch t := raw.(type) {
	case []any:
		list = t
	case []uint64:
		for _, id := range t {
			list = append(list, id)
		}
	default:
		return nil, "必须是素材 ID 数组"
	}
	ids := make([]uint64, 0, len(list))
	for _, item := range list {
		id, ok := asAssetID(item)
		if !ok {
			return nil, "包含无效的素材 ID"
		}
		ids = append(ids, id)
	}
	return ids, ""
}

func (c Capabilities) firstOp() string {
	if len(c.Ops) == 0 {
		return ""
	}
	return c.Ops[0]
}

func (e ParamEntry) label(fallback string) string {
	if e.Label != "" {
		return e.Label
	}
	return fallback
}

func kindLabel(kind string) string {
	switch kind {
	case "image":
		return "图片"
	case "video":
		return "视频"
	}
	return "音频"
}

// isBlank 空白字符串等同于没传（前端表单清空输入框时会传 ""）。
func isBlank(raw any) bool {
	s, ok := raw.(string)
	return ok && strings.TrimSpace(s) == ""
}

// normalizeParam 校验并规范化一个生成参数的值，返回中文错误后缀（空表示通过）。
func normalizeParam(f ParamField, raw any) (any, string) {
	switch f.Type {
	case ParamEnum:
		return normalizeEnumParam(f, raw)
	case ParamNumber:
		return normalizeNumberParam(f, raw)
	case ParamBoolean:
		return normalizeBoolParam(raw)
	}
	return nil, "的类型不受支持"
}

func normalizeEnumParam(f ParamField, raw any) (any, string) {
	opt, ok := matchOption(f.Options, raw)
	if !ok {
		labels := make([]string, 0, len(f.Options))
		for _, o := range f.Options {
			labels = append(labels, stringify(o))
		}
		return nil, "必须是 " + strings.Join(labels, " / ") + " 之一"
	}
	// 数字选项统一成 float64，避免下游（插件的 ctx.input）拿到 int / float64 混杂的类型
	if n, isNum := toFloat(opt); isNum {
		return n, ""
	}
	return opt, ""
}

func normalizeNumberParam(f ParamField, raw any) (any, string) {
	n, ok := asNumber(raw)
	switch {
	case !ok || n != math.Trunc(n):
		return nil, "必须是整数"
	case f.Min != nil && n < float64(*f.Min):
		return nil, fmt.Sprintf("不能小于 %d", *f.Min)
	case f.Max != nil && n > float64(*f.Max):
		return nil, fmt.Sprintf("不能大于 %d", *f.Max)
	}
	return n, ""
}

// normalizeBoolParam 接受布尔值，以及表单常见的 "true" / "false" 字符串。
func normalizeBoolParam(raw any) (any, string) {
	switch t := raw.(type) {
	case bool:
		return t, ""
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true":
			return true, ""
		case "false":
			return false, ""
		}
	}
	return nil, "必须是 true 或 false"
}

// asNumber 接受 float64 / 各种整型 / json.Number，拒绝 NaN、Inf 与字符串。
func asNumber(raw any) (float64, bool) {
	n, ok := toFloat(raw)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// MediaRef 是任务输入里的一个参考素材：Key 是 images / videos / audios，Index 是它在数组里的下标。
type MediaRef struct {
	Key   string
	Kind  string // image / video / audio
	Index int
	ID    uint64
}

// Ref 返回插件里使用的文件引用名，如 "images.0"（完整引用写作 input:images.0）。
func (r MediaRef) Ref() string { return r.Key + "." + strconv.Itoa(r.Index) }

// mediaRefs 按 images / videos / audios 的顺序提取任务输入里的参考素材。
// 数组元素可以是 uint64、数字、json.Number 或数字字符串（落库后的任务输入经 JSON 解码，素材 id 是字符串或 float64）；
// 无效元素跳过。
func mediaRefs(in map[string]any) []MediaRef {
	var out []MediaRef
	for _, m := range MediaKinds {
		var list []any
		switch t := in[m.Key].(type) {
		case []any:
			list = t
		case []uint64:
			for _, id := range t {
				list = append(list, id)
			}
		case []string:
			for _, id := range t {
				list = append(list, id)
			}
		}
		for i, item := range list {
			if id, ok := asAssetID(item); ok {
				out = append(out, MediaRef{Key: m.Key, Kind: m.Kind, Index: i, ID: id})
			}
		}
	}
	return out
}

// asAssetID 把 JSON 数字、float64、json.Number、数字字符串规范成 uint64 素材 id；0 与负数无效。
func asAssetID(raw any) (uint64, bool) {
	switch t := raw.(type) {
	case uint64:
		return t, t > 0
	case uint:
		return uint64(t), t > 0
	case uint32:
		return uint64(t), t > 0
	case int:
		return uint64(t), t > 0
	case int32:
		return uint64(t), t > 0
	case int64:
		return uint64(t), t > 0
	case float64:
		// float64 超过 2^53 已经不能精确表示整数，直接拒绝，避免悄悄指向别的素材
		if t <= 0 || t != math.Trunc(t) || t > maxSafeAssetID {
			return 0, false
		}
		return uint64(t), true
	case float32:
		return asAssetID(float64(t))
	case json.Number:
		return parseAssetIDString(t.String())
	case string:
		return parseAssetIDString(t)
	}
	return 0, false
}

func parseAssetIDString(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// parseNumberString 解析数字字符串。
func parseNumberString(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
