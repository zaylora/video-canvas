package drama

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// AdaptAsset 是原稿里引用到的资产，只给模型名字和节点 id，转写不改资产事实。
type AdaptAsset struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

// AdaptInput 是转写一个片段所需的全部输入；引用的资产从原稿里解析，不另传。
type AdaptInput struct {
	Source          string
	Seconds         int
	Ratio           string
	Style           string
	ValidationError string
}

// AdaptOutput 是转写结果，只有一份目标平台写法的提示词。
type AdaptOutput struct {
	Prompt string `json:"prompt"`
}

// Validate 拒绝空提示词；引用与时间轴依赖原稿和时长，由 ValidateAdapt 校验。
func (o AdaptOutput) Validate() error {
	if !nonemptyText(o.Prompt) {
		return invalidOutput("prompt 不能为空")
	}
	return nil
}

// AdaptFormatSupported 只有通用和 H3 需要转写；Seedance 就是原稿本身。
func AdaptFormatSupported(format string) bool {
	return format == PromptFormatGeneric || format == PromptFormatH3
}

// SourceHash 是原稿全文 UTF-8 的 SHA-256 小写 hex，和前端 Web Crypto 算法一致，用来判断转写稿是否过期。
func SourceHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// AdaptPrompt 拼出转写某个片段的完整提示词；不支持的格式返回错误。
func AdaptPrompt(format string, in AdaptInput) (string, error) {
	if !AdaptFormatSupported(format) {
		return "", errors.New("不支持转写为这种格式：" + format)
	}
	vars := map[string]any{
		"source_prompt": in.Source,
		"seconds":       in.Seconds,
		"assets":        PromptAssets(in.Source),
		"ratio":         in.Ratio,
		"style":         in.Style,
	}
	if in.ValidationError != "" {
		vars["validation_error"] = in.ValidationError
	}
	return Prompt("adapt_"+format, vars)
}

// PromptAssets 按出现顺序列出提示词里的 `@[名](节点id)` 引用，同一节点只列一次。
func PromptAssets(prompt string) []AdaptAsset {
	out := []AdaptAsset{}
	seen := map[string]bool{}
	for _, m := range storyboardMention.FindAllStringSubmatch(prompt, -1) {
		if seen[m[2]] {
			continue
		}
		seen[m[2]] = true
		out = append(out, AdaptAsset{NodeID: m[2], Name: m[1]})
	}
	return out
}

// h3Fields 是 H3 写法必须出现的三个字段名。
var h3Fields = []string{"integrated_multimodal_description:", "overall_soundscape:", "non_diegetic_music:"}

// ValidateAdapt 检查转写稿：非空；原稿的每个引用至少保留一次、不出现原稿没有的引用（参考图靠引用挂载，丢了或编了都会错）；
// H3 三个字段齐全；时间轴不超过片段时长。返回的错误会作为 validation_error 交给模型重写一次。
func ValidateAdapt(format, source, adapted string, seconds int) error {
	if !nonemptyText(adapted) {
		return invalidOutput("prompt 不能为空")
	}
	want := map[string]string{}
	for _, a := range PromptAssets(source) {
		want[a.NodeID] = a.Name
	}
	got := map[string]bool{}
	for _, a := range PromptAssets(adapted) {
		if _, ok := want[a.NodeID]; !ok {
			return invalidOutput("出现了原稿没有的引用 @[" + a.Name + "](" + a.NodeID + ")，只能使用原稿里的引用")
		}
		got[a.NodeID] = true
	}
	for id, name := range want {
		if !got[id] {
			return invalidOutput("丢了原稿里的引用 @[" + name + "](" + id + ")，每个引用都要原样保留至少一次")
		}
	}
	if format == PromptFormatH3 {
		for _, field := range h3Fields {
			if !strings.Contains(adapted, field) {
				return invalidOutput("缺少字段 " + strings.TrimSuffix(field, ":"))
			}
		}
	}
	if end, over := timelineOverrun(adapted, seconds); over {
		return invalidOutput("时间轴写到" + strconv.FormatFloat(end, 'f', -1, 64) + "秒，超过片段时长" + strconv.Itoa(seconds) + "秒")
	}
	return nil
}
