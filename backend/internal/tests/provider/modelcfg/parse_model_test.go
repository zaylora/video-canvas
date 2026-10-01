// 本文件：ParseModel 与 ValidateInputSchema 的单元测试：成功路径、每条规则的失败、未知字段、类型错误、
// channels 数量、kind=text、Issue 路径精确性、默认值与大整数参数。

package modelcfg_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

// mcLoadFixture 读取 testdata 下的夹具并解码成通用 map，供用例按需修改。
func mcLoadFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func mcMarshal(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mcDig 取嵌套 map，路径用点分隔。
func mcDig(m map[string]any, path string) map[string]any {
	cur := m
	for _, p := range strings.Split(path, ".") {
		cur = cur[p].(map[string]any)
	}
	return cur
}

// mcChannel0 取 channels[0]。
func mcChannel0(m map[string]any) map[string]any {
	return m["channels"].([]any)[0].(map[string]any)
}

func mcHasIssue(issues []modelcfg.Issue, path string) (modelcfg.Issue, bool) {
	for _, i := range issues {
		if i.Path == path {
			return i, true
		}
	}
	return modelcfg.Issue{}, false
}

func mcIssueList(issues []modelcfg.Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Path+"："+i.Message)
	}
	return out
}

func mcIssuePaths(issues []modelcfg.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Path)
	}
	return out
}

func TestParseModel_夹具通过校验(t *testing.T) {
	b, err := os.ReadFile("testdata/model_video.json")
	if err != nil {
		t.Fatal(err)
	}
	m, issues := modelcfg.ParseModel(b)
	if len(issues) > 0 {
		t.Fatalf("应通过校验，实际问题：%v", mcIssueList(issues))
	}
	if m.Key != "kling-i2v" || m.Kind != modelcfg.KindVideo || m.Pricing.Billing != modelcfg.BillingPerSecond || m.Pricing.PerSecond != 2 || m.Deadline.D().Minutes() != 30 || !m.Enabled || m.Sort != 100 {
		t.Fatalf("解析结果不符：%+v", m)
	}
	if len(m.Channels) != 1 || m.Channels[0].Channel != "newapi-main" || m.Channels[0].UpstreamModel != "kling-v2-master" {
		t.Fatalf("channels 不符：%+v", m.Channels)
	}
	// capabilities.params 保序
	var names []string
	for _, e := range m.Capabilities.Params {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "aspect_ratio,resolution,duration,generate_audio" {
		t.Fatalf("capabilities.params 顺序错误：%v", names)
	}
	if len(m.Capabilities.Ops) != 3 || !m.Capabilities.Refs.Image.On || m.Capabilities.Prompt.MaxLength != 2000 {
		t.Fatalf("capabilities 解析不符：%+v", m.Capabilities)
	}
	if v := m.Params["max_tokens"]; v != 2000.0 {
		t.Fatalf("params 错误：%v", m.Params)
	}
	if m.Hint == "" {
		t.Fatal("hint 应被保留")
	}
}

func TestParseModel_补默认值(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"没有 deadline", func(m map[string]any) { delete(m, "deadline") }},
		{"deadline 为 null", func(m map[string]any) { m["deadline"] = nil }},
		{"deadline 为空字符串", func(m map[string]any) { m["deadline"] = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := mcLoadFixture(t, "model_video.json")
			tt.mutate(m)
			got, issues := modelcfg.ParseModel(mcMarshal(t, m))
			if len(issues) > 0 {
				t.Fatalf("应通过：%v", mcIssueList(issues))
			}
			if got.Deadline.D() != modelcfg.DefaultModelDeadline {
				t.Fatalf("deadline = %v，期望默认 30m", got.Deadline.D())
			}
		})
	}
}

func TestParseModel_deadline边界(t *testing.T) {
	tests := []struct {
		deadline any
		wantOK   bool
	}{
		{"1s", true}, {"24h", true}, {"90m", true}, {3600, true}, // 数字按秒
		{"0s", false}, {0, false}, {"-5m", false}, {"24h1s", false}, {"48h", false}, {"soon", false},
	}
	for _, tt := range tests {
		m := mcLoadFixture(t, "model_video.json")
		m["deadline"] = tt.deadline
		_, issues := modelcfg.ParseModel(mcMarshal(t, m))
		if tt.wantOK && len(issues) > 0 {
			t.Errorf("deadline=%v 应通过：%v", tt.deadline, mcIssueList(issues))
		}
		if !tt.wantOK {
			if _, ok := mcHasIssue(issues, "deadline"); !ok {
				t.Errorf("deadline=%v 应报 deadline 的 Issue，实际 %v", tt.deadline, mcIssueList(issues))
			}
		}
	}
}

func TestParseModel_大整数参数不丢精度(t *testing.T) {
	m := `{"key":"m","kind":"image","label":"x","channels":[{"channel":"c","upstream_model":"u"}],
	"params":{"webappId":2093984571330498561,"n":5,"nested":{"big":2093984571330498562},"list":[1,2.5]},
	"capabilities":{"ops":["t2i"],"prompt":{"max_length":100}},"pricing":{"billing":"per_call","unit":1}}`
	got, issues := modelcfg.ParseModel([]byte(m))
	if len(issues) > 0 {
		t.Fatalf("应通过：%v", mcIssueList(issues))
	}
	if got.Params["webappId"] != 2093984571330498561 {
		t.Fatalf("大整数丢精度：%#v", got.Params["webappId"])
	}
	if got.Params["n"] != 5.0 {
		t.Fatalf("小整数应为 float64：%#v", got.Params["n"])
	}
	if got.Params["nested"].(map[string]any)["big"] != 2093984571330498562 {
		t.Fatalf("嵌套大整数丢精度：%#v", got.Params["nested"])
	}
	if l := got.Params["list"].([]any); l[0] != 1.0 || l[1] != 2.5 {
		t.Fatalf("数组里的数字应为 float64：%#v", l)
	}
}

// mcCase 是“改一处夹具 -> 期望某条路径出现 Issue”的用例。
type mcCase struct {
	name     string
	mutate   func(m map[string]any)
	wantPath string
	wantMsg  string // 非空时，Issue 消息必须包含它
}

// mcRunCases 逐条执行用例：失败时返回 nil 配置，且 Issue 路径精确。
func mcRunCases(t *testing.T, tests []mcCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mcLoadFixture(t, "model_video.json")
			tt.mutate(m)
			got, issues := modelcfg.ParseModel(mcMarshal(t, m))
			if got != nil || len(issues) == 0 {
				t.Fatalf("期望校验失败")
			}
			is, ok := mcHasIssue(issues, tt.wantPath)
			if !ok {
				t.Fatalf("期望 Issue 路径 %q，实际：%v", tt.wantPath, mcIssueList(issues))
			}
			if tt.wantMsg != "" && !strings.Contains(is.Message, tt.wantMsg) {
				t.Fatalf("Issue 消息 %q 应包含 %q", is.Message, tt.wantMsg)
			}
		})
	}
}

func TestParseModel_基础字段与channels规则(t *testing.T) {
	mcRunCases(t, []mcCase{
		// —— key / kind / label / deadline ——
		{"key 含空格", func(m map[string]any) { m["key"] = "a b" }, "key", ""},
		{"key 为空", func(m map[string]any) { m["key"] = "" }, "key", ""},
		{"key 以点开头", func(m map[string]any) { m["key"] = ".a" }, "key", ""},
		{"key 超过 128 位", func(m map[string]any) { m["key"] = strings.Repeat("a", 129) }, "key", "128"},
		{"kind 非法", func(m map[string]any) { m["kind"] = "3d" }, "kind", "video / image / audio / text"},
		{"kind 为空", func(m map[string]any) { m["kind"] = "" }, "kind", ""},
		{"kind 大小写不符", func(m map[string]any) { m["kind"] = "Video" }, "kind", ""},
		{"label 为空", func(m map[string]any) { m["label"] = "" }, "label", ""},
		{"label 只有空白", func(m map[string]any) { m["label"] = "   " }, "label", ""},
		{"deadline 格式错误", func(m map[string]any) { m["deadline"] = "soon" }, "deadline", "时长格式"},
		{"deadline 过长", func(m map[string]any) { m["deadline"] = "48h" }, "deadline", "24h"},
		{"deadline 为 0", func(m map[string]any) { m["deadline"] = "0s" }, "deadline", "大于 0"},

		// —— channels ——
		{"缺少 channels", func(m map[string]any) { delete(m, "channels") }, "channels", "首期只支持一个渠道"},
		{"channels 为空数组", func(m map[string]any) { m["channels"] = []any{} }, "channels", "首期只支持一个渠道"},
		{"channels 有两个元素", func(m map[string]any) {
			m["channels"] = append(m["channels"].([]any), map[string]any{"channel": "other", "upstream_model": "x"})
		}, "channels", "首期只支持一个渠道"},
		{"channels 不是数组", func(m map[string]any) { m["channels"] = "newapi-main" }, "channels", "数组"},
		{"channels 元素不是对象", func(m map[string]any) { m["channels"] = []any{"newapi-main"} }, "channels[0]", "对象"},
		{"channel 为空", func(m map[string]any) { mcChannel0(m)["channel"] = "" }, "channels[0].channel", ""},
		{"channel 含大写", func(m map[string]any) { mcChannel0(m)["channel"] = "NewAPI" }, "channels[0].channel", ""},
		{"channel 含下划线", func(m map[string]any) { mcChannel0(m)["channel"] = "new_api" }, "channels[0].channel", ""},
		{"channel 以连字符开头", func(m map[string]any) { mcChannel0(m)["channel"] = "-a" }, "channels[0].channel", ""},
		{"channel 超过 64 位", func(m map[string]any) { mcChannel0(m)["channel"] = strings.Repeat("a", 65) }, "channels[0].channel", "64"},
		{"channel 缺失", func(m map[string]any) { delete(mcChannel0(m), "channel") }, "channels[0].channel", ""},
		{"upstream_model 为空", func(m map[string]any) { mcChannel0(m)["upstream_model"] = "" }, "channels[0].upstream_model", "不能为空"},
		{"upstream_model 只有空白", func(m map[string]any) { mcChannel0(m)["upstream_model"] = "  " }, "channels[0].upstream_model", "不能为空"},
		{"upstream_model 缺失", func(m map[string]any) { delete(mcChannel0(m), "upstream_model") }, "channels[0].upstream_model", "不能为空"},
		{"upstream_model 超过 128 个字符", func(m map[string]any) {
			mcChannel0(m)["upstream_model"] = strings.Repeat("模", 129)
		}, "channels[0].upstream_model", "128"},

		// —— params ——
		{"params 不是对象", func(m map[string]any) { m["params"] = 5 }, "params", "对象"},
		{"params 是数组", func(m map[string]any) { m["params"] = []any{1} }, "params", "对象"},
	})
}

func TestParseModel_能力规则(t *testing.T) {
	mcRunCases(t, []mcCase{
		// —— 生成方式 ——
		{"没有生成方式", func(m map[string]any) { mcCaps(m)["ops"] = []any{} }, "capabilities.ops", "至少"},
		{"生成方式非法", func(m map[string]any) { mcCaps(m)["ops"] = []any{"t2i"} }, "capabilities.ops[0]", "t2v"},
		{"生成方式重复", func(m map[string]any) { mcCaps(m)["ops"] = []any{"t2v", "t2v"} }, "capabilities.ops[1]", "重复"},
		{"文本模型不能有生成方式", func(m map[string]any) {
			mcAsText(m)
			mcCaps(m)["ops"] = []any{"t2v"}
		}, "capabilities.ops", "没有生成方式"},
		// —— 参考素材 ——
		{"参考图数量超过 50", func(m map[string]any) { mcDig(m, "capabilities.refs.image")["max"] = 51 }, "capabilities.refs.image.max", "1 – 50"},
		{"开启后数量为 0", func(m map[string]any) { mcDig(m, "capabilities.refs.image")["max"] = 0 }, "capabilities.refs.image.max", "1 – 50"},
		{"大小超过 500MB", func(m map[string]any) { mcDig(m, "capabilities.refs.audio")["max_mb"] = 501 }, "capabilities.refs.audio.max_mb", "500"},
		{"大小为 0", func(m map[string]any) { mcDig(m, "capabilities.refs.image")["max_mb"] = 0 }, "capabilities.refs.image.max_mb", "1"},
		{"图生需要开启图片素材", func(m map[string]any) { mcDig(m, "capabilities.refs.image")["on"] = false }, "capabilities.refs.image.on", "图生"},
		{"全能参考至少开一种素材", func(m map[string]any) {
			mcCaps(m)["ops"] = []any{"omni"}
			mcDig(m, "capabilities.refs.image")["on"] = false
			mcDig(m, "capabilities.refs.audio")["on"] = false
		}, "capabilities.refs", "至少"},
		{"图片模型不能开视频素材", func(m map[string]any) {
			m["kind"] = "image"
			mcCaps(m)["ops"] = []any{"t2i", "i2i"}
			mcDig(m, "capabilities.refs.video")["on"] = true
		}, "capabilities.refs.video.on", "不接收"},
		// —— 提示词 ——
		{"提示词上限为 0", func(m map[string]any) { mcDig(m, "capabilities.prompt")["max_length"] = 0 }, "capabilities.prompt.max_length", "1 –"},
		{"提示词上限过大", func(m map[string]any) { mcDig(m, "capabilities.prompt")["max_length"] = 1000001 }, "capabilities.prompt.max_length", "1000000"},
		// —— 生成参数 ——
		{"参数名含大写", func(m map[string]any) {
			mcParams(m)["Ratio"] = map[string]any{"type": "boolean", "label": "x", "open": true}
		}, "capabilities.params.Ratio", "参数名"},
		{"参数名是保留键", func(m map[string]any) {
			mcParams(m)["prompt"] = map[string]any{"type": "boolean", "label": "x", "open": true}
		}, "capabilities.params.prompt", "保留"},
		{"参数类型非法", func(m map[string]any) { mcParam(m, "duration")["type"] = "text" }, "capabilities.params.duration.type", "enum"},
		{"参数缺 label", func(m map[string]any) { mcParam(m, "duration")["label"] = "" }, "capabilities.params.duration.label", ""},
		{"enum 没有可选值", func(m map[string]any) { mcParam(m, "resolution")["options"] = []any{} }, "capabilities.params.resolution.options", "1 –"},
		{"enum 可选值重复", func(m map[string]any) { mcParam(m, "resolution")["options"] = []any{"720P", "720P"} }, "capabilities.params.resolution.options[1]", "重复"},
		{"enum 可选值类型错误", func(m map[string]any) { mcParam(m, "resolution")["options"] = []any{true} }, "capabilities.params.resolution.options[0]", "字符串或数字"},
		{"enum 默认值不在可选值内", func(m map[string]any) { mcParam(m, "resolution")["default"] = "4K" }, "capabilities.params.resolution.default", "可选值之一"},
		{"enum 没有默认值", func(m map[string]any) { delete(mcParam(m, "resolution"), "default") }, "capabilities.params.resolution.default", "默认值"},
		{"number 缺 min / max", func(m map[string]any) { delete(mcParam(m, "duration"), "max") }, "capabilities.params.duration", "min"},
		{"number 最大值超过 3600", func(m map[string]any) { mcParam(m, "duration")["max"] = 3601 }, "capabilities.params.duration.min", "3600"},
		{"number 最小值大于最大值", func(m map[string]any) { mcParam(m, "duration")["min"] = 20 }, "capabilities.params.duration.min", ""},
		{"number 默认值超出范围", func(m map[string]any) { mcParam(m, "duration")["default"] = 13 }, "capabilities.params.duration.default", "4 – 12"},
		{"number 默认值不是步长整数倍", func(m map[string]any) {
			mcParam(m, "duration")["step"] = 2
			mcParam(m, "duration")["default"] = 5
		}, "capabilities.params.duration.default", "整数倍"},
		{"number 步长小于 1", func(m map[string]any) { mcParam(m, "duration")["step"] = 0 }, "capabilities.params.duration.step", "1"},
		{"number 不能做规格维度", func(m map[string]any) { mcParam(m, "duration")["spec"] = true }, "capabilities.params.duration.spec", "number"},
		{"enum 写了 min", func(m map[string]any) { mcParam(m, "resolution")["min"] = 1 }, "capabilities.params.resolution", "number"},
		{"number 写了 options", func(m map[string]any) { mcParam(m, "duration")["options"] = []any{1} }, "capabilities.params.duration.options", "enum"},
		{"boolean 默认值类型错误", func(m map[string]any) { mcParam(m, "generate_audio")["default"] = "yes" }, "capabilities.params.generate_audio.default", "true / false"},
		{"不开放的参数必须有默认值", func(m map[string]any) {
			mcParams(m)["flag"] = map[string]any{"type": "boolean", "label": "F", "open": false}
		}, "capabilities.params.flag.default", "不开放"},
		{"fanout 必须是 enum", func(m map[string]any) {
			mcParam(m, "duration")["fanout"] = true
		}, "capabilities.params.duration.fanout", "enum"},
		{"fanout 取值超过 8", func(m map[string]any) {
			mcParams(m)["count"] = map[string]any{"type": "enum", "label": "数量", "open": true, "options": []any{1, 9}, "default": 1, "fanout": true}
		}, "capabilities.params.count.options", "1 – 8"},
		{"fanout 至多一个", func(m map[string]any) {
			for _, n := range []string{"c1", "c2"} {
				mcParams(m)[n] = map[string]any{"type": "enum", "label": "数量", "open": true, "options": []any{1, 2}, "default": 1, "fanout": true}
			}
		}, "capabilities.params", "至多一个"},
		// —— 文本上下文 ——
		{"文本模型缺 context", func(m map[string]any) { mcAsText(m); delete(mcCaps(m), "context") }, "capabilities.context", "必须设置"},
		{"最大输出不小于窗口", func(m map[string]any) {
			mcAsText(m)
			mcCaps(m)["context"] = map[string]any{"window": 1000, "output": 1000}
		}, "capabilities.context.output", "小于"},
		{"最大输出过小", func(m map[string]any) {
			mcAsText(m)
			mcCaps(m)["context"] = map[string]any{"window": 8000, "output": 100}
		}, "capabilities.context.output", "256"},
		{"窗口过大", func(m map[string]any) {
			mcAsText(m)
			mcCaps(m)["context"] = map[string]any{"window": 10000001, "output": 1000}
		}, "capabilities.context.window", "10000000"},
		{"非文本不能有 context", func(m map[string]any) { mcCaps(m)["context"] = map[string]any{"window": 8000, "output": 1000} }, "capabilities.context", "文本"},
		{"非文本不能有 system", func(m map[string]any) { mcCaps(m)["system"] = "你好" }, "capabilities.system", "文本"},
	})
}

// mcCaps 取夹具的 capabilities。
func mcCaps(m map[string]any) map[string]any { return mcDig(m, "capabilities") }

// mcParams 取夹具的 capabilities.params。
func mcParams(m map[string]any) map[string]any { return mcDig(m, "capabilities.params") }

// mcParam 取夹具里的一个生成参数。
func mcParam(m map[string]any, name string) map[string]any { return mcParams(m)[name].(map[string]any) }

// mcAsText 把夹具改成合法的文本模型（无生成方式、无素材、有 context、没有生成参数）。
func mcAsText(m map[string]any) {
	m["kind"] = "text"
	m["capabilities"] = map[string]any{
		"prompt":  map[string]any{"max_length": 8000},
		"context": map[string]any{"window": 128000, "output": 8192},
	}
	m["pricing"] = map[string]any{"billing": "token", "token": map[string]any{"in": 2, "out": 8}}
}

// mcPricing 取夹具的 pricing。
func mcPricing(m map[string]any) map[string]any { return mcDig(m, "pricing") }

func TestParseModel_未知字段与类型错误(t *testing.T) {
	mcRunCases(t, []mcCase{
		{"顶层未知字段", func(m map[string]any) { m["mapp"] = 1 }, "mapp", "未知字段"},
		{"旧的 provider 字段已不存在", func(m map[string]any) { m["provider"] = "runninghub" }, "provider", "未知字段"},
		{"旧的 mapping 字段已不存在", func(m map[string]any) { m["mapping"] = map[string]any{} }, "mapping", "未知字段"},
		{"旧的 output 字段已不存在", func(m map[string]any) { m["output"] = map[string]any{} }, "output", "未知字段"},
		{"channels 元素里的未知字段", func(m map[string]any) { mcChannel0(m)["weight"] = 1 }, "channels[0].weight", "未知字段"},
		{"参数定义里的未知属性", func(m map[string]any) { mcParam(m, "duration")["requried"] = true }, "capabilities.params.duration.requried", "未知字段"},
		{"旧的 input_schema 已不存在", func(m map[string]any) { m["input_schema"] = map[string]any{} }, "input_schema", "未知字段"},
		{"capabilities 不是对象", func(m map[string]any) { m["capabilities"] = []any{} }, "capabilities", "对象"},
		{"capabilities.params 不是对象", func(m map[string]any) { mcCaps(m)["params"] = []any{} }, "capabilities.params", "对象"},
		{"素材上限不是整数", func(m map[string]any) { mcDig(m, "capabilities.refs.image")["max"] = 1.5 }, "capabilities.refs.image.max", "整数"},
		{"key 不是字符串", func(m map[string]any) { m["key"] = 5 }, "key", "字符串"},
		{"label 不是字符串", func(m map[string]any) { m["label"] = true }, "label", "字符串"},
		{"旧的 credits 已不存在", func(m map[string]any) { m["credits"] = 10 }, "credits", "未知字段"},
		{"价格是小数", func(m map[string]any) { mcPricing(m)["per_second"] = 1.5 }, "pricing.per_second", "整数"},
		{"价格是字符串", func(m map[string]any) { mcPricing(m)["per_second"] = "2" }, "pricing.per_second", "整数"},
		{"enabled 不是布尔", func(m map[string]any) { m["enabled"] = "yes" }, "enabled", "布尔"},
		{"sort 是字符串", func(m map[string]any) { m["sort"] = "1" }, "sort", "整数"},
		{"deadline 是布尔", func(m map[string]any) { m["deadline"] = true }, "deadline", "字符串"},
		{"参数 open 不是布尔", func(m map[string]any) { mcParam(m, "duration")["open"] = "true" }, "capabilities.params.duration.open", "布尔"},
		{"参数 min 不是整数", func(m map[string]any) { mcParam(m, "duration")["min"] = "1" }, "capabilities.params.duration.min", "整数"},
		{"channel 不是字符串", func(m map[string]any) { mcChannel0(m)["channel"] = 1 }, "channels[0].channel", "字符串"},
	})
}

func TestParseModel_JSON语法与结构错误(t *testing.T) {
	tests := []struct {
		name, body string
	}{
		{"空正文", ""},
		{"只有空白", "  \n"},
		{"非法 JSON", `{"key": 1,`},
		{"数组", `[]`},
		{"字符串", `"x"`},
		{"多余内容", `{} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, issues := modelcfg.ParseModel([]byte(tt.body))
			if m != nil || len(issues) != 1 || issues[0].Path != "" {
				t.Fatalf("期望单条根路径 Issue，实际 %v", mcIssueList(issues))
			}
		})
	}
}

func TestParseModel_多个问题一次报出(t *testing.T) {
	m := mcLoadFixture(t, "model_video.json")
	m["key"] = "a b"
	m["kind"] = "3d"
	mcPricing(m)["per_second"] = -1
	m["channels"] = []any{
		map[string]any{"channel": "BAD", "upstream_model": ""},
		map[string]any{"channel": "ok", "upstream_model": "x"},
	}
	mcParam(m, "duration")["type"] = "text"
	_, issues := modelcfg.ParseModel(mcMarshal(t, m))
	for _, p := range []string{"key", "kind", "pricing.per_second", "channels", "channels[0].channel", "channels[0].upstream_model", "capabilities.params.duration.type"} {
		if _, ok := mcHasIssue(issues, p); !ok {
			t.Errorf("缺少路径 %s，实际 %v", p, mcIssueList(issues))
		}
	}
}

func TestParseModel_重复参数名(t *testing.T) {
	body := `{"key":"m","kind":"image","label":"x","channels":[{"channel":"c","upstream_model":"u"}],"capabilities":{
		"ops":["t2i"],"prompt":{"max_length":100},"params":{
		"a":{"type":"boolean","label":"A","open":true},"a":{"type":"boolean","label":"B","open":true}}},
		"pricing":{"billing":"per_call","unit":1}}`
	_, issues := modelcfg.ParseModel([]byte(body))
	if is, ok := mcHasIssue(issues, "capabilities.params.a"); !ok || !strings.Contains(is.Message, "重复") {
		t.Fatalf("期望重复参数名 Issue，实际：%v", mcIssueList(issues))
	}
}

func TestParseModel_合法变体(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"没有 params", func(m map[string]any) { delete(m, "params") }},
		{"params 为空对象", func(m map[string]any) { m["params"] = map[string]any{} }},
		{"params 为 null", func(m map[string]any) { m["params"] = nil }},
		{"没有 hint", func(m map[string]any) { delete(m, "hint") }},
		{"hint 为空", func(m map[string]any) { m["hint"] = "" }},
		{"没有生成参数（按次计费）", func(m map[string]any) {
			delete(mcCaps(m), "params")
			m["pricing"] = map[string]any{"billing": "per_call", "unit": 10}
		}},
		{"只留文生", func(m map[string]any) {
			mcCaps(m)["ops"] = []any{"t2v"}
			mcDig(m, "capabilities.refs.image")["on"] = false
			delete(mcPricing(m), "tiers")
		}},
		{"没有成本", func(m map[string]any) { delete(mcPricing(m), "cost") }},
		{"没有规格价格", func(m map[string]any) { delete(mcPricing(m), "tiers") }},
		{"按次计费", func(m map[string]any) {
			m["pricing"] = map[string]any{"billing": "per_call", "unit": 10,
				"tiers": []any{map[string]any{"on": true, "when": map[string]any{"generate_audio": false}, "unit": 8}}}
		}},
		{"有参考视频作为规格条件", func(m map[string]any) {
			mcDig(m, "capabilities.refs.video")["on"] = true
			mcDig(m, "capabilities.refs.video")["max"] = 1
			mcDig(m, "capabilities.refs.video")["max_mb"] = 100
			mcPricing(m)["tiers"] = []any{map[string]any{"on": true, "when": map[string]any{"ref_video": true}, "unit": 1}}
		}},
		{"kind=text", func(m map[string]any) {
			mcAsText(m)
			mcCaps(m)["system"] = "你是一名分镜师"
		}},
		{"kind=text 按次计费", func(m map[string]any) {
			mcAsText(m)
			m["pricing"] = map[string]any{"billing": "per_call", "unit": 1}
		}},
		{"kind=image", func(m map[string]any) {
			m["kind"] = "image"
			mcCaps(m)["ops"] = []any{"t2i", "i2i"}
			mcDig(m, "capabilities.refs.audio")["on"] = false
			m["pricing"] = map[string]any{"billing": "per_call", "unit": 4}
		}},
		{"kind=audio", func(m map[string]any) {
			m["kind"] = "audio"
			m["capabilities"] = map[string]any{"prompt": map[string]any{"max_length": 4096}}
			m["pricing"] = map[string]any{"billing": "per_call", "unit": 2}
		}},
		{"key 含点、下划线、大写", func(m map[string]any) { m["key"] = "Kling_v2.master-1" }},
		{"key 恰好 128 位", func(m map[string]any) { m["key"] = strings.Repeat("a", 128) }},
		{"channel 恰好 64 位", func(m map[string]any) { mcChannel0(m)["channel"] = strings.Repeat("a", 64) }},
		{"upstream_model 恰好 128 个字符", func(m map[string]any) { mcChannel0(m)["upstream_model"] = strings.Repeat("模", 128) }},
		{"upstream_model 含斜杠与冒号", func(m map[string]any) { mcChannel0(m)["upstream_model"] = "org/model:v1" }},
		{"数字枚举与生成数量", func(m map[string]any) {
			mcParams(m)["count"] = map[string]any{"type": "enum", "label": "生成数量", "open": true, "options": []any{1, 2, 4}, "default": 1, "fanout": true, "unit": "个"}
		}},
		{"不开放的参数有默认值", func(m map[string]any) {
			mcParams(m)["seed"] = map[string]any{"type": "number", "label": "种子", "open": false, "min": 1, "max": 100, "default": 7}
		}},
		{"number 不写步长", func(m map[string]any) { delete(mcParam(m, "duration"), "step") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mcLoadFixture(t, "model_video.json")
			tt.mutate(m)
			got, issues := modelcfg.ParseModel(mcMarshal(t, m))
			if len(issues) > 0 || got == nil {
				t.Fatalf("应通过：%v", mcIssueList(issues))
			}
		})
	}
}

func TestParseModel_定价规则(t *testing.T) {
	tier := func(m map[string]any, when map[string]any) {
		mcPricing(m)["tiers"] = []any{map[string]any{"on": true, "when": when, "unit": 1}}
	}
	mcRunCases(t, []mcCase{
		{"没有 pricing", func(m map[string]any) { delete(m, "pricing") }, "pricing.billing", "per_call"},
		{"计费方式非法", func(m map[string]any) { mcPricing(m)["billing"] = "monthly" }, "pricing.billing", "per_call"},
		{"按秒计费需要 duration 参数", func(m map[string]any) { delete(mcParams(m), "duration") }, "pricing.billing", "duration"},
		{"默认价为 0", func(m map[string]any) { mcPricing(m)["per_second"] = 0 }, "pricing.per_second", "大于 0"},
		{"价格超过上限", func(m map[string]any) { mcPricing(m)["per_second"] = 1000001 }, "pricing.per_second", "1000000"},
		{"按次计费默认价为 0", func(m map[string]any) { m["pricing"] = map[string]any{"billing": "per_call"} }, "pricing.unit", "大于 0"},
		{"视频不能按 Token 计费", func(m map[string]any) {
			m["pricing"] = map[string]any{"billing": "token", "token": map[string]any{"in": 1, "out": 1}}
		}, "pricing.billing", "文本"},
		{"Token 计费缺单价", func(m map[string]any) { mcAsText(m); delete(mcPricing(m), "token") }, "pricing.token", "输入价"},
		{"Token 单价都为 0", func(m map[string]any) {
			mcAsText(m)
			mcPricing(m)["token"] = map[string]any{"in": 0, "out": 0}
		}, "pricing.token", "都为 0"},
		{"Token 计费不支持规格价格", func(m map[string]any) {
			mcAsText(m)
			mcPricing(m)["tiers"] = []any{map[string]any{"on": true, "when": map[string]any{"op": "x"}, "unit": 1}}
		}, "pricing.tiers", "不支持"},
		{"规格价格没有条件", func(m map[string]any) { tier(m, map[string]any{}) }, "pricing.tiers[0].when", "至少"},
		{"规格价格条件的参数不存在", func(m map[string]any) { tier(m, map[string]any{"fps": 30}) }, "pricing.tiers[0].when.fps", "不存在"},
		{"规格价格条件的参数不是规格维度", func(m map[string]any) {
			tier(m, map[string]any{"aspect_ratio": "16:9"})
		}, "pricing.tiers[0].when.aspect_ratio", "规格价格维度"},
		{"规格价格条件的可选值被取消", func(m map[string]any) {
			tier(m, map[string]any{"resolution": "4K"})
		}, "pricing.tiers[0].when.resolution", "4K 已不可选"},
		{"布尔条件不是布尔", func(m map[string]any) {
			tier(m, map[string]any{"generate_audio": "yes"})
		}, "pricing.tiers[0].when.generate_audio", "true / false"},
		{"生成方式条件未勾选", func(m map[string]any) {
			mcCaps(m)["ops"] = []any{"t2v", "i2v"}
			tier(m, map[string]any{"op": "omni"})
		}, "pricing.tiers[0].when.op", "已勾选"},
		{"参考视频未开启却作为条件", func(m map[string]any) {
			tier(m, map[string]any{"ref_video": true})
		}, "pricing.tiers[0].when.ref_video", "没有开启"},
		{"规格价格为负", func(m map[string]any) {
			mcPricing(m)["tiers"] = []any{map[string]any{"on": true, "when": map[string]any{"op": "t2v"}, "unit": -1}}
		}, "pricing.tiers[0].unit", "0 –"},
		{"成本为负", func(m map[string]any) { mcDig(m, "pricing.cost")["per_second"] = -1 }, "pricing.cost.per_second", "0 –"},
		{"规格价格里的未知字段", func(m map[string]any) {
			mcPricing(m)["tiers"] = []any{map[string]any{"on": true, "when": map[string]any{"op": "t2v"}, "unit": 1, "x": 1}}
		}, "pricing.tiers[0].x", "未知字段"},
	})
}
