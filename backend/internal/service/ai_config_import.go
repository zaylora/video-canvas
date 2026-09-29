package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
)

const (
	// aiRunningHubNodeInfoPath 是获取 AI 应用可改节点的接口（本机 runninghub skill 用的同一个）。
	aiRunningHubNodeInfoPath = "/api/webapp/apiCallDemo"
	aiImportTimeout          = 30 * time.Second
	aiImportMaxBody          = 2 << 20 // 响应体上限 2MB
)

// aiWebappIDRe 限制 webappId 为纯数字：它会拼进请求 URL，不允许任何其他字符。
var aiWebappIDRe = regexp.MustCompile(`^[0-9]{1,32}$`)

// ImportNode 是 RunningHub 应用的一个可改节点原始信息，供运营勾选参考。
type ImportNode struct {
	NodeID      string `json:"node_id"`
	FieldName   string `json:"field_name"`
	FieldValue  any    `json:"field_value"` // 工作流里当前的值
	FieldType   string `json:"field_type,omitempty"`
	Description string `json:"description,omitempty"`
	InputName   string `json:"input_name"` // 草稿里为它起的输入名
	InputType   string `json:"input_type"` // 自动推断的 input_schema 类型
}

// ImportResult 是自动导入的结果：Model 草稿建议（不落库）+ 节点原始信息 + 提示。
type ImportResult struct {
	Draft    json.RawMessage `json:"draft"`
	Nodes    []ImportNode    `json:"nodes"`
	Warnings []string        `json:"warnings"`
}

// ImportRunningHub 按 webappId 调用 apiCallDemo 获取可改节点，生成 Model 草稿建议。只返回建议，不落库，
// 运营确认、修改后自己用“保存草稿”接口存。kind 为空默认 video；providerKey 为空默认 runninghub。
func (s *AIConfigService) ImportRunningHub(ctx context.Context, webappID, kind, providerKey string) (*ImportResult, error) {
	// 1. 参数：webappId 只允许数字（会拼进 URL）；kind / provider 补默认值
	webappID = strings.TrimSpace(webappID)
	if !aiWebappIDRe.MatchString(webappID) {
		return nil, errcode.ErrInvalidParams.WithMsg("webappId 必须是纯数字")
	}
	if kind == "" {
		kind = model.KindVideo
	}
	if kind != model.KindVideo && kind != model.KindImage && kind != model.KindAudio {
		return nil, errcode.ErrInvalidParams.WithMsg("kind 只能是 video / image / audio")
	}
	if providerKey == "" {
		providerKey = "runninghub"
	}
	if s.httpClients == nil {
		return nil, errcode.ErrInternal.WithMsg("导入功能未启用")
	}

	// 2. 平台配置：取 base_url / allowed_hosts / 凭证名。已发布优先，没有发布过就用草稿（首次配置平台时也能导入）
	pcfg, _, err := s.loadProvider(ctx, providerKey, aiProviderPublishedThenDraft)
	if err != nil {
		return nil, err
	}
	if pcfg.Auth.Secret == "" {
		return nil, errcode.ErrConfigInvalid.WithMsg("平台没有配置 auth.secret，无法调用 apiCallDemo")
	}
	apiKey, err := s.Get(ctx, pcfg.Auth.Secret)
	if errors.Is(err, ErrAISecretNotSet) {
		return nil, errcode.ErrSecretNotSet.WithMsg(fmt.Sprintf("凭证 %q 尚未设置", pcfg.Auth.Secret))
	}
	if errors.Is(err, ErrAISecretKeyMissing) {
		return nil, errcode.ErrInternal.WithMsg(ErrAISecretKeyMissing.Error())
	}
	if err != nil {
		return nil, err
	}

	// 3. 调用 apiCallDemo：走受 SSRF 防护的客户端（host 白名单 + 内网拦截）。
	//    apiKey 放在查询串里（与 runninghub skill 一致），所以错误信息必须脱敏后才能返回或记录
	body, err := s.fetchRunningHubNodes(ctx, pcfg, apiKey, webappID)
	if err != nil {
		return nil, err
	}

	// 4. 宽容解析响应，得到节点列表
	nodes, err := aiParseNodeInfoList(body)
	if err != nil {
		return nil, errcode.ErrConfigInvalid.WithMsg("解析 RunningHub 响应失败：" + aiRedactString(err.Error(), []string{apiKey}))
	}
	if len(nodes) == 0 {
		return nil, errcode.ErrConfigInvalid.WithMsg("该应用没有可修改的节点，请先在 RunningHub 网页端成功运行一次该应用")
	}

	// 5. 生成草稿建议
	res, err := aiBuildImportDraft(webappID, kind, providerKey, nodes)
	if err != nil {
		return nil, err
	}
	logger.Info("导入 RunningHub 应用", zap.String("webapp_id", webappID), zap.Int("nodes", len(nodes)))
	return res, nil
}

// fetchRunningHubNodes 请求 apiCallDemo 并返回响应体；任何错误信息都会先替换掉 apiKey。
func (s *AIConfigService) fetchRunningHubNodes(ctx context.Context, pcfg *dsl.ProviderConfig, apiKey, webappID string) ([]byte, error) {
	base, err := url.Parse(strings.TrimRight(pcfg.BaseURL, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, errcode.ErrConfigInvalid.WithMsg("平台的 base_url 不合法")
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + aiRunningHubNodeInfoPath
	q := url.Values{}
	q.Set("apiKey", apiKey)
	q.Set("webappId", webappID)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, aiImportTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errcode.ErrInternal.WithMsg("构造 RunningHub 请求失败")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	client := s.httpClients.NewClient(pcfg.AllowedHosts, aiImportTimeout)
	resp, err := client.Do(req)
	if err != nil {
		// url.Error 会带上完整 URL（含 apiKey），必须脱敏
		return nil, errcode.ErrInternal.WithMsg("调用 RunningHub 失败：" + aiRedactString(err.Error(), []string{apiKey}))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiImportMaxBody))
	if err != nil {
		return nil, errcode.ErrInternal.WithMsg("读取 RunningHub 响应失败：" + aiRedactString(err.Error(), []string{apiKey}))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errcode.ErrInternal.WithMsg(fmt.Sprintf("RunningHub 返回 HTTP %d：%s", resp.StatusCode,
			aiRedactString(aiTruncate(string(data), 200), []string{apiKey})))
	}
	return data, nil
}

// aiRawNode 是解析出的一个节点，字段名做过大小写 / 下划线归一。
type aiRawNode struct {
	NodeID      string
	FieldName   string
	FieldValue  any
	FieldType   string
	Description string
}

// aiParseNodeInfoList 宽容地从响应里取出节点列表：
// 兼容 data.nodeInfoList / 顶层 nodeInfoList / data 直接是数组 / 顶层直接是数组；
// 字段名大小写、下划线差异都不敏感；响应带 code 且不是成功码时返回带 msg 的错误。
func aiParseNodeInfoList(body []byte) ([]aiRawNode, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // 保留大整数精度（nodeId 可能很长）
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("响应不是合法的 JSON")
	}

	var list []any
	switch v := root.(type) {
	case []any:
		list = v
	case map[string]any:
		if code, ok := aiLookup(v, "code"); ok && !aiIsSuccessCode(code) {
			msg, _ := aiLookup(v, "msg", "message", "errorMessage")
			return nil, fmt.Errorf("RunningHub 返回错误 code=%v msg=%v", code, msg)
		}
		if l, ok := aiLookupList(v, "nodeInfoList"); ok {
			list = l
		} else if data, ok := aiLookup(v, "data"); ok {
			switch d := data.(type) {
			case []any:
				list = d
			case map[string]any:
				list, _ = aiLookupList(d, "nodeInfoList")
			}
		}
	default:
		return nil, fmt.Errorf("响应结构无法识别")
	}

	nodes := make([]aiRawNode, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fieldName := aiString(aiFirstOf(m, "fieldName"))
		if fieldName == "" {
			continue // 没有字段名的节点没法映射
		}
		val, _ := aiLookup(m, "fieldValue", "value")
		nodes = append(nodes, aiRawNode{
			NodeID:      aiString(aiFirstOf(m, "nodeId", "id")),
			FieldName:   fieldName,
			FieldValue:  aiNormalizeNumber(val),
			FieldType:   aiString(aiFirstOf(m, "fieldType", "type")),
			Description: aiString(aiFirstOf(m, "description", "desc", "nodeName", "descriptionEn")),
		})
	}
	return nodes, nil
}

// aiNormKey 把键名归一：小写并去掉下划线、短横线。
func aiNormKey(k string) string {
	k = strings.ToLower(k)
	k = strings.ReplaceAll(k, "_", "")
	return strings.ReplaceAll(k, "-", "")
}

// aiLookup 大小写 / 下划线不敏感地按候选名取值，按候选名顺序优先。
func aiLookup(m map[string]any, names ...string) (any, bool) {
	norm := make(map[string]any, len(m))
	for k, v := range m {
		norm[aiNormKey(k)] = v
	}
	for _, n := range names {
		if v, ok := norm[aiNormKey(n)]; ok {
			return v, true
		}
	}
	return nil, false
}

func aiFirstOf(m map[string]any, names ...string) any {
	v, _ := aiLookup(m, names...)
	return v
}

func aiLookupList(m map[string]any, name string) ([]any, bool) {
	v, ok := aiLookup(m, name)
	if !ok {
		return nil, false
	}
	l, ok := v.([]any)
	return l, ok
}

// aiIsSuccessCode 判断响应 code 是否表示成功：0 / "0" / 200 / "200"。
func aiIsSuccessCode(code any) bool {
	switch c := code.(type) {
	case json.Number:
		return c.String() == "0" || c.String() == "200"
	case string:
		return c == "0" || c == "200" || strings.EqualFold(c, "success")
	case nil:
		return true
	}
	return false
}

// aiString 把标量转成字符串；nil 得到空串。
func aiString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(t)
	}
}

// aiNormalizeNumber 把 json.Number 转成 int64 / float64，方便后续判断类型和输出。
func aiNormalizeNumber(v any) any {
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return n.String()
}

// aiCoerceDefault 把平台给的当前值转成与字段类型一致的默认值：RunningHub 的数字 / 开关值常以字符串返回（"1024"、"true"），
// 直接写进 number / boolean 字段会校验不过；转不出来就不设默认值。文本类空串也保留，避免映射出 null。
func aiCoerceDefault(t string, v any) any {
	switch t {
	case dsl.FieldText:
		return aiString(v)
	case dsl.FieldNumber:
		switch x := v.(type) {
		case int64, float64, int:
			return x
		}
		s := aiString(v)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return nil
	case dsl.FieldBoolean:
		if b, ok := v.(bool); ok {
			return b
		}
		if b, err := strconv.ParseBool(aiString(v)); err == nil {
			return b
		}
		return nil
	}
	return v
}

func aiTruncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// aiInferType 推断一个节点在 input_schema 里的类型：
// 先看平台给的 fieldType，再看当前值是不是数字 / 布尔，然后看字段名（提示词优先，其次视频 / 音频 / 图片），其余按 text。
// 数字 / 布尔先于按名字推断媒体，避免 image_strength 这类数值参数被误判成图片。
func aiInferType(fieldName, fieldType string, value any) string {
	ft := strings.ToLower(strings.TrimSpace(fieldType))
	switch {
	case strings.Contains(ft, "image"):
		return dsl.FieldImage
	case strings.Contains(ft, "video"):
		return dsl.FieldVideo
	case strings.Contains(ft, "audio"):
		return dsl.FieldAudio
	}
	switch ft {
	case "int", "integer", "float", "double", "number", "long":
		return dsl.FieldNumber
	case "bool", "boolean", "switch":
		return dsl.FieldBoolean
	case "string", "text", "prompt", "str":
		return dsl.FieldText
	}
	switch value.(type) {
	case int64, float64, int, json.Number:
		return dsl.FieldNumber
	case bool:
		return dsl.FieldBoolean
	}
	name := strings.ToLower(fieldName)
	switch {
	case strings.Contains(name, "prompt") || name == "text":
		return dsl.FieldText
	case strings.Contains(name, "video") || strings.Contains(name, "视频"):
		return dsl.FieldVideo
	case strings.Contains(name, "audio") || strings.Contains(name, "voice") || strings.Contains(name, "music") ||
		strings.Contains(name, "sound") || strings.Contains(name, "音频"):
		return dsl.FieldAudio
	case strings.Contains(name, "image") || strings.Contains(name, "img") || strings.Contains(name, "picture") ||
		strings.Contains(name, "photo") || strings.Contains(name, "mask") ||
		strings.Contains(name, "图片") || strings.Contains(name, "图像") || strings.Contains(name, "首帧") || strings.Contains(name, "尾帧"):
		return dsl.FieldImage
	}
	return dsl.FieldText
}

func aiIsMedia(t string) bool {
	return t == dsl.FieldImage || t == dsl.FieldVideo || t == dsl.FieldAudio
}

func aiIsPromptLike(fieldName string) bool {
	n := strings.ToLower(fieldName)
	return n == "text" || strings.Contains(n, "prompt")
}

var aiNonIdentRe = regexp.MustCompile(`[^a-z0-9_]+`)

// aiInputName 由字段名生成输入名（小写蛇形），保证在 used 里唯一；重名时追加节点号。
func aiInputName(fieldName, nodeID string, used map[string]bool) string {
	name := strings.Trim(aiNonIdentRe.ReplaceAllString(strings.ToLower(fieldName), "_"), "_")
	if name == "" {
		name = "field"
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "f_" + name
	}
	if used[name] {
		name = name + "_" + aiNonIdentRe.ReplaceAllString(strings.ToLower(nodeID), "_")
	}
	for base, i := name, 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	used[name] = true
	return name
}

// aiOutputTypes 是各种类的默认产物扩展名，用来生成 output.select。
var aiOutputTypes = map[string]string{
	model.KindVideo: "['mp4', 'webm', 'mov']",
	model.KindImage: "['png', 'jpg', 'jpeg', 'webp']",
	model.KindAudio: "['mp3', 'wav', 'flac', 'm4a', 'ogg']",
}

// aiBuildImportDraft 由节点列表生成 Model 草稿：每个节点一个输入字段 + 一条 mapping，
// 值预填成 ${ input.xxx }，媒体字段用 ${ files.xxx }；credits / deadline 用保守默认值，enabled=false。
func aiBuildImportDraft(webappID, kind, providerKey string, nodes []aiRawNode) (*ImportResult, error) {
	used := map[string]bool{}
	schema := dsl.InputSchema{}
	mapping := make([]map[string]any, 0, len(nodes))
	infos := make([]ImportNode, 0, len(nodes))
	warnings := []string{
		"kind 按参数默认为 " + kind + "，请按工作流的实际输出确认",
		"credits 默认 10，deadline 默认 30m，enabled 默认 false，请按需调整",
		"所有节点都已暴露为输入字段，请删除不需要让用户修改的节点（保留工作流原值），并修改 label",
	}

	for _, n := range nodes {
		t := aiInferType(n.FieldName, n.FieldType, n.FieldValue)
		promptLike := t == dsl.FieldText && aiIsPromptLike(n.FieldName)

		// 输入名：文本节点 text 更名为 prompt，与画布节点已有的 prompt 字段对齐
		nameSrc := n.FieldName
		if promptLike && strings.EqualFold(n.FieldName, "text") && !used["prompt"] {
			nameSrc = "prompt"
		}
		name := aiInputName(nameSrc, n.NodeID, used)

		label := n.Description
		if label == "" {
			label = n.FieldName
		}
		field := dsl.InputField{Type: t, Label: label}
		switch {
		case aiIsMedia(t):
			field.Required = true
			field.Port = t
		case promptLike:
			field.Required = true
			def := aiString(n.FieldValue)
			field.MaxLength = max(2000, len([]rune(def))) // 平台工作流自带的默认提示词可能很长，上限不能比它还小
			field.Port = dsl.FieldText
			field.Default = def
		default:
			field.Advanced = true
			field.Default = aiCoerceDefault(t, n.FieldValue)
		}
		schema = append(schema, dsl.InputFieldEntry{Name: name, InputField: field})

		src := "input"
		if aiIsMedia(t) {
			src = "files"
		}
		mapping = append(mapping, map[string]any{
			"nodeId":     n.NodeID,
			"fieldName":  n.FieldName,
			"fieldValue": fmt.Sprintf("${ %s.%s }", src, name),
		})
		infos = append(infos, ImportNode{
			NodeID: n.NodeID, FieldName: n.FieldName, FieldValue: n.FieldValue, FieldType: n.FieldType,
			Description: n.Description, InputName: name, InputType: t,
		})
	}

	cfg := dsl.ModelConfig{
		Key:         "rh-" + webappID,
		Kind:        kind,
		Provider:    providerKey,
		Label:       fmt.Sprintf("RunningHub 应用 %s（请修改名称）", webappID),
		Credits:     10,
		Deadline:    dsl.Duration(30 * time.Minute),
		Enabled:     false,
		Sort:        100,
		Params:      map[string]any{"webappId": webappID, "instanceType": "default"},
		InputSchema: schema,
		Mapping:     map[string]any{"nodeInfoList": mapping},
		Output:      dsl.OutputConfig{Select: fmt.Sprintf("filter(outputs, .type in %s)", aiOutputTypes[kind]), Media: kind},
	}
	draft, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成草稿失败：%w", err)
	}
	return &ImportResult{Draft: draft, Nodes: infos, Warnings: warnings}, nil
}
