package drama

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ErrOutputInvalid 表示模型输出不符合步骤的 JSON 结构或语义约束。
var ErrOutputInvalid = errors.New("模型输出无法解析")

// AnalyzeOutput 是原文或多个研读分块的汇总结果。
type AnalyzeOutput struct {
	SourceKind   string `json:"source_kind"`
	Summary      string `json:"summary"`
	CharactersMD string `json:"characters_md"`
}

// OutlineEpisode 是大纲中的分集；已分集剧本的 ScriptMD 直接保存原文整理结果。
type OutlineEpisode struct {
	No       int    `json:"no"`
	Title    string `json:"title"`
	Synopsis string `json:"synopsis"`
	ScriptMD string `json:"script_md"`
}

// OutlineOutput 是项目大纲、人物小传和连续编号的分集目录。
type OutlineOutput struct {
	OutlineMD    string           `json:"outline_md"`
	CharactersMD string           `json:"characters_md"`
	Episodes     []OutlineEpisode `json:"episodes"`
}

// ScriptOutput 是一次单集生成的完整剧本。
type ScriptOutput struct {
	ScriptMD string `json:"script_md"`
}

// Asset 是一次拆解识别出的角色造型、场景或道具；Episodes 空数组表示全集。
type Asset struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Look        string   `json:"look"`
	Aliases     []string `json:"aliases"`
	Episodes    []int    `json:"episodes"`
	Description string   `json:"description"`
	VoicePrompt string   `json:"voice_prompt"`
}

// BreakdownOutput 是所选集使用的视觉资产清单。
type BreakdownOutput struct {
	Assets []Asset `json:"assets"`
}

// AssetPrompt 是只针对指定新节点生成的图片正向提示词。
type AssetPrompt struct {
	NodeID string `json:"node_id"`
	Prompt string `json:"prompt"`
}

// AssetPromptsOutput 是新建资产节点的提示词集合。
type AssetPromptsOutput struct {
	Prompts []AssetPrompt `json:"prompts"`
}

// Validate 检查研读来源分类和必需正文。
func (o AnalyzeOutput) Validate() error {
	if o.SourceKind != "script" && o.SourceKind != "novel" {
		return invalidOutput("source_kind 必须为 script 或 novel")
	}
	if !nonemptyText(o.Summary) || !nonemptyText(o.CharactersMD) {
		return invalidOutput("summary 与 characters_md 必须为非空文本")
	}
	return nil
}

// Validate 检查大纲、人物和 1–100 集连续编号；ScriptMD 可为空等待分集生成。
func (o OutlineOutput) Validate() error {
	if !nonemptyText(o.OutlineMD) || !nonemptyText(o.CharactersMD) {
		return invalidOutput("outline_md 与 characters_md 必须为非空文本")
	}
	if len(o.Episodes) < 1 || len(o.Episodes) > 100 {
		return invalidOutput("episodes 必须包含 1–100 集")
	}
	for i, episode := range o.Episodes {
		if episode.No != i+1 {
			return invalidOutput("episodes 集号必须从 1 开始按顺序连续排列")
		}
		if !nonemptyText(episode.Title) || !nonemptyText(episode.Synopsis) || !utf8.ValidString(episode.ScriptMD) {
			return invalidOutput(fmt.Sprintf("第 %d 集标题、梗概或剧本文本无效", episode.No))
		}
	}
	return nil
}

// Validate 检查单集剧本包含非空正文。
func (o ScriptOutput) Validate() error {
	if !nonemptyText(o.ScriptMD) {
		return invalidOutput("script_md 必须为非空文本")
	}
	return nil
}

// Validate 检查资产类型、字段长度与集号；同名造型由画布按身份合并，业务范围由调用方验证。
func (o BreakdownOutput) Validate() error {
	if len(o.Assets) == 0 {
		return invalidOutput("assets 必须包含至少一个资产")
	}
	for _, asset := range o.Assets {
		if err := asset.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate 检查单个资产结构；空别名、重复集号和超出角色卡字段长度的值均拒绝。
func (a Asset) Validate() error {
	if a.Type != "character" && a.Type != "scene" && a.Type != "prop" {
		return invalidOutput("资产 type 必须为 character、scene 或 prop")
	}
	if !nonemptyText(a.Name) || utf8.RuneCountInString(a.Name) > 64 {
		return invalidOutput("资产 name 必须为 1–64 字")
	}
	if !utf8.ValidString(a.Look) || utf8.RuneCountInString(a.Look) > 64 {
		return invalidOutput("资产 look 必须为不超过 64 字的文本")
	}
	if !nonemptyText(a.Description) {
		return invalidOutput("资产 description 必须为非空文本")
	}
	if a.Type == "character" && utf8.RuneCountInString(a.Description) > 500 {
		return invalidOutput("角色 description 不得超过 500 字")
	}
	if !utf8.ValidString(a.VoicePrompt) || utf8.RuneCountInString(a.VoicePrompt) > 500 {
		return invalidOutput("资产 voice_prompt 必须为不超过 500 字的文本")
	}
	if a.Type != "character" && a.VoicePrompt != "" {
		return invalidOutput("场景和道具的 voice_prompt 必须为空串")
	}
	return validateAssetLists(a.Aliases, a.Episodes)
}

// validateAssetLists 检查别名和集号均为非 null 数组，且内容合法、没有重复。
func validateAssetLists(names []string, numbers []int) error {
	if names == nil || numbers == nil {
		return invalidOutput("资产 aliases 与 episodes 必须为数组，不能为 null")
	}
	aliases := make(map[string]bool, len(names))
	for _, alias := range names {
		if !nonemptyText(alias) || utf8.RuneCountInString(alias) > 64 || aliases[normalized(alias)] {
			return invalidOutput("资产 aliases 必须是互不重复的 1–64 字名称")
		}
		aliases[normalized(alias)] = true
	}
	episodes := make(map[int]bool, len(numbers))
	for _, no := range numbers {
		if no < 1 || no > 100 || episodes[no] {
			return invalidOutput("资产 episodes 必须是互不重复的 1–100 集号")
		}
		episodes[no] = true
	}
	return nil
}

// Validate 检查提示词非空且每个节点只出现一次；完整覆盖由调用方对照新节点检查。
func (o AssetPromptsOutput) Validate() error {
	if len(o.Prompts) == 0 {
		return invalidOutput("prompts 必须包含至少一个节点提示词")
	}
	seen := make(map[string]bool, len(o.Prompts))
	for _, prompt := range o.Prompts {
		if !nonemptyText(prompt.NodeID) || prompt.NodeID != strings.TrimSpace(prompt.NodeID) || !nonemptyText(prompt.Prompt) {
			return invalidOutput("node_id 与 prompt 必须为非空文本，node_id 不得带首尾空格")
		}
		if seen[prompt.NodeID] {
			return invalidOutput("prompts 的 node_id 不得重复")
		}
		seen[prompt.NodeID] = true
	}
	return nil
}

// DecodeOutput 严格解析单个 JSON 对象，拒绝未知、重复、缺失或 null 字段，并执行步骤语义校验。
// 返回对应步骤的非指针输出，包括 StoryboardOutput。
func DecodeOutput(step string, data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, invalidOutput("JSON 必须使用 UTF-8 编码")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := checkJSONValue(decoder, 0); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, invalidOutput("输出只能包含一个 JSON 对象")
	}
	definition, err := definitionForStep(step)
	if err != nil {
		return nil, err
	}
	if err = definition.checkFields(data); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, definition.value); err != nil {
		return nil, invalidOutput("JSON 字段类型不符合步骤定义")
	}
	if err = definition.value.Validate(); err != nil {
		return nil, err
	}
	return outputValue(definition.value)
}

// outputDefinition 描述一个步骤的目标类型、顶层字段和可选对象数组字段。
type outputDefinition struct {
	value      interface{ Validate() error }
	fields     []string
	listKey    string
	itemFields []string
}

// definitionForStep 列出流水线允许的步骤，避免未知步骤或字段被静默接受。
func definitionForStep(step string) (outputDefinition, error) {
	switch step {
	case "analyze":
		return outputDefinition{value: &AnalyzeOutput{}, fields: []string{"source_kind", "summary", "characters_md"}}, nil
	case "outline":
		return outputDefinition{value: &OutlineOutput{}, fields: []string{"outline_md", "characters_md", "episodes"},
			listKey: "episodes", itemFields: []string{"no", "title", "synopsis", "script_md"}}, nil
	case "scripts":
		return outputDefinition{value: &ScriptOutput{}, fields: []string{"script_md"}}, nil
	case "breakdown":
		return outputDefinition{value: &BreakdownOutput{}, fields: []string{"assets"}, listKey: "assets",
			itemFields: []string{"type", "name", "look", "aliases", "episodes", "description", "voice_prompt"}}, nil
	case "asset_prompts":
		return outputDefinition{value: &AssetPromptsOutput{}, fields: []string{"prompts"},
			listKey: "prompts", itemFields: []string{"node_id", "prompt"}}, nil
	case "storyboard":
		return outputDefinition{value: &StoryboardOutput{}, fields: []string{"segments"},
			listKey: "segments", itemFields: []string{"seconds", "prompt", "shots"}}, nil
	default:
		return outputDefinition{}, invalidOutput("不支持的流水线步骤")
	}
}

// checkFields 先验证必需字段，避免零值掩盖缺少字段、null 和大小写错误。
func (d outputDefinition) checkFields(data []byte) error {
	object, err := requireObject(data, d.fields)
	if err != nil || d.listKey == "" {
		return err
	}
	var items []json.RawMessage
	if err = json.Unmarshal(object[d.listKey], &items); err != nil {
		return invalidOutput(d.listKey + " 必须为数组")
	}
	for _, item := range items {
		if _, err = requireObject(item, d.itemFields); err != nil {
			return err
		}
	}
	if _, ok := d.value.(*StoryboardOutput); ok {
		return checkStoryboardShots(data)
	}
	return nil
}

// outputValue 将已经校验的目标指针转回契约约定的值类型。
func outputValue(output any) (any, error) {
	switch value := output.(type) {
	case *AnalyzeOutput:
		return *value, nil
	case *OutlineOutput:
		return *value, nil
	case *ScriptOutput:
		return *value, nil
	case *BreakdownOutput:
		return *value, nil
	case *AssetPromptsOutput:
		return *value, nil
	case *StoryboardOutput:
		return *value, nil
	default:
		return nil, invalidOutput("不支持的输出类型")
	}
}

// requireObject 要求对象恰好包含约定字段，且每个值都不是 null。
func requireObject(data []byte, fields []string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, invalidOutput("输出及数组条目必须为 JSON 对象")
	}
	if len(object) != len(fields) {
		return nil, invalidOutput("JSON 对象包含未知字段或缺少必需字段")
	}
	for _, field := range fields {
		value, ok := object[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, invalidOutput(field + " 为必需字段且不能为 null")
		}
	}
	return object, nil
}

// checkJSONValue 检查语法和所有层级的重复键，避免解析器静默覆盖模型输出。
func checkJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return invalidOutput("JSON 嵌套过深")
	}
	token, err := decoder.Token()
	if err != nil {
		return invalidOutput("输出不是有效 JSON")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		if err = checkJSONObject(decoder, depth); err != nil {
			return err
		}
	case '[':
		for decoder.More() {
			if err = checkJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return invalidOutput("JSON 分隔符无效")
	}
	closing, err := decoder.Token()
	if err != nil || (delimiter == '{' && closing != json.Delim('}')) || (delimiter == '[' && closing != json.Delim(']')) {
		return invalidOutput("JSON 对象或数组未闭合")
	}
	return nil
}

// checkJSONObject 扫描对象字段并检测重复键，值递归交给统一语法检查。
func checkJSONObject(decoder *json.Decoder, depth int) error {
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return invalidOutput("JSON 对象键无效")
		}
		name, isString := key.(string)
		if !isString || seen[name] {
			return invalidOutput("JSON 对象含重复或无效字段名")
		}
		seen[name] = true
		if err = checkJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// normalized 与资产合并规则一致，忽略名称或造型的大小写和首尾空白。
func normalized(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

// nonemptyText 检查 Unicode 文本至少包含一个非空白字符。
func nonemptyText(text string) bool {
	return utf8.ValidString(text) && strings.TrimSpace(text) != ""
}

// invalidOutput 为可反馈给模型的校验原因附上统一哨兵。
func invalidOutput(reason string) error {
	return fmt.Errorf("%w：%s", ErrOutputInvalid, reason)
}
