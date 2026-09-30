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
	if m.Key != "kling-i2v" || m.Kind != modelcfg.KindVideo || m.Credits != 10 || m.Deadline.D().Minutes() != 30 || !m.Enabled || m.Sort != 100 {
		t.Fatalf("解析结果不符：%+v", m)
	}
	if len(m.Channels) != 1 || m.Channels[0].Channel != "newapi-main" || m.Channels[0].UpstreamModel != "kling-v2-master" {
		t.Fatalf("channels 不符：%+v", m.Channels)
	}
	// input_schema 保序
	var names []string
	for _, e := range m.InputSchema {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "prompt,image,duration" {
		t.Fatalf("input_schema 顺序错误：%v", names)
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
	"input_schema":{"a":{"type":"text","label":"A"}}}`
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
		// —— key / kind / label / credits / deadline ——
		{"key 含空格", func(m map[string]any) { m["key"] = "a b" }, "key", ""},
		{"key 为空", func(m map[string]any) { m["key"] = "" }, "key", ""},
		{"key 以点开头", func(m map[string]any) { m["key"] = ".a" }, "key", ""},
		{"key 超过 128 位", func(m map[string]any) { m["key"] = strings.Repeat("a", 129) }, "key", "128"},
		{"kind 非法", func(m map[string]any) { m["kind"] = "3d" }, "kind", "video / image / audio / text"},
		{"kind 为空", func(m map[string]any) { m["kind"] = "" }, "kind", ""},
		{"kind 大小写不符", func(m map[string]any) { m["kind"] = "Video" }, "kind", ""},
		{"label 为空", func(m map[string]any) { m["label"] = "" }, "label", ""},
		{"label 只有空白", func(m map[string]any) { m["label"] = "   " }, "label", ""},
		{"credits 为负", func(m map[string]any) { m["credits"] = -1 }, "credits", "负数"},
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

func TestParseModel_输入schema规则(t *testing.T) {
	mcRunCases(t, []mcCase{
		{"字段类型非法", func(m map[string]any) { mcDig(m, "input_schema.prompt")["type"] = "string" }, "input_schema.prompt.type", ""},
		{"字段缺 label", func(m map[string]any) { mcDig(m, "input_schema.prompt")["label"] = "" }, "input_schema.prompt.label", ""},
		{"字段名含连字符", func(m map[string]any) {
			mcDig(m, "input_schema")["first-frame"] = map[string]any{"type": "image", "label": "x"}
		}, "input_schema.first-frame", "字段名"},
		{"字段名以数字开头", func(m map[string]any) {
			mcDig(m, "input_schema")["1st"] = map[string]any{"type": "image", "label": "x"}
		}, "input_schema.1st", "字段名"},
		{"enum 没有 options", func(m map[string]any) { delete(mcDig(m, "input_schema.duration"), "options") }, "input_schema.duration.options", "至少"},
		{"enum 选项 value 类型错误", func(m map[string]any) {
			mcDig(m, "input_schema.duration")["options"] = []any{map[string]any{"value": true, "label": "x"}}
		}, "input_schema.duration.options[0].value", "字符串或数字"},
		{"enum 选项重复", func(m map[string]any) {
			mcDig(m, "input_schema.duration")["options"] = []any{
				map[string]any{"value": 5, "label": "5 秒"}, map[string]any{"value": 5.0, "label": "又 5 秒"},
			}
		}, "input_schema.duration.options[1].value", "重复"},
		{"enum 选项 5 与 \"5\" 视为重复", func(m map[string]any) {
			mcDig(m, "input_schema.duration")["options"] = []any{
				map[string]any{"value": 5, "label": "5 秒"}, map[string]any{"value": "5", "label": "五"},
			}
		}, "input_schema.duration.options[1].value", "重复"},
		{"enum 选项缺 label", func(m map[string]any) {
			mcDig(m, "input_schema.duration")["options"] = []any{map[string]any{"value": 5, "label": ""}}
			delete(mcDig(m, "input_schema.duration"), "default")
		}, "input_schema.duration.options[0].label", ""},
		{"enum 默认值不在选项内", func(m map[string]any) { mcDig(m, "input_schema.duration")["default"] = 7 }, "input_schema.duration.default", "options"},
		{"非 enum 写了 options", func(m map[string]any) {
			mcDig(m, "input_schema.prompt")["options"] = []any{map[string]any{"value": "a", "label": "a"}}
		}, "input_schema.prompt.options", "enum"},
		{"number min 大于 max", func(m map[string]any) {
			mcDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "min": 10, "max": 1}
		}, "input_schema.n.min", "max"},
		{"text 写了 min", func(m map[string]any) { mcDig(m, "input_schema.prompt")["min"] = 1 }, "input_schema.prompt.min", "number"},
		{"text 写了 max", func(m map[string]any) { mcDig(m, "input_schema.prompt")["max"] = 1 }, "input_schema.prompt.max", "number"},
		{"number 写了 max_length", func(m map[string]any) {
			mcDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "max_length": 5}
		}, "input_schema.n.max_length", "text"},
		{"max_length 为负", func(m map[string]any) { mcDig(m, "input_schema.prompt")["max_length"] = -1 }, "input_schema.prompt.max_length", "负数"},
		{"port 非法", func(m map[string]any) { mcDig(m, "input_schema.prompt")["port"] = "file" }, "input_schema.prompt.port", ""},
		{"text 字段 port 不是 text", func(m map[string]any) { mcDig(m, "input_schema.prompt")["port"] = "image" }, "input_schema.prompt.port", "text"},
		{"媒体字段 port 与类型不符", func(m map[string]any) { mcDig(m, "input_schema.image")["port"] = "video" }, "input_schema.image.port", "image"},
		{"number 字段不能有 port", func(m map[string]any) {
			mcDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "port": "text"}
		}, "input_schema.n.port", ""},
		{"媒体字段不能有默认值", func(m map[string]any) { mcDig(m, "input_schema.image")["default"] = 1 }, "input_schema.image.default", "媒体"},
		{"text 默认值类型错误", func(m map[string]any) { mcDig(m, "input_schema.prompt")["default"] = 5 }, "input_schema.prompt.default", "字符串"},
		{"text 默认值超长", func(m map[string]any) {
			f := mcDig(m, "input_schema.prompt")
			f["max_length"] = 2
			f["default"] = "abc"
		}, "input_schema.prompt.default", "max_length"},
		{"number 默认值小于 min", func(m map[string]any) {
			mcDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "min": 5, "default": 1}
		}, "input_schema.n.default", "min"},
		{"boolean 默认值类型错误", func(m map[string]any) {
			mcDig(m, "input_schema")["b"] = map[string]any{"type": "boolean", "label": "B", "default": "yes"}
		}, "input_schema.b.default", "true / false"},
	})
}

func TestParseModel_未知字段与类型错误(t *testing.T) {
	mcRunCases(t, []mcCase{
		{"顶层未知字段", func(m map[string]any) { m["mapp"] = 1 }, "mapp", "未知字段"},
		{"旧的 provider 字段已不存在", func(m map[string]any) { m["provider"] = "runninghub" }, "provider", "未知字段"},
		{"旧的 mapping 字段已不存在", func(m map[string]any) { m["mapping"] = map[string]any{} }, "mapping", "未知字段"},
		{"旧的 output 字段已不存在", func(m map[string]any) { m["output"] = map[string]any{} }, "output", "未知字段"},
		{"channels 元素里的未知字段", func(m map[string]any) { mcChannel0(m)["weight"] = 1 }, "channels[0].weight", "未知字段"},
		{"字段定义里的未知属性", func(m map[string]any) { mcDig(m, "input_schema.prompt")["requried"] = true }, "input_schema.prompt.requried", "未知字段"},
		{"选项里的未知属性", func(m map[string]any) {
			mcDig(m, "input_schema.duration")["options"] = []any{map[string]any{"value": 5, "label": "5", "x": 1}}
		}, "input_schema.duration.options[0].x", "未知字段"},
		{"input_schema 不是对象", func(m map[string]any) { m["input_schema"] = []any{} }, "input_schema", "对象"},
		{"key 不是字符串", func(m map[string]any) { m["key"] = 5 }, "key", "字符串"},
		{"label 不是字符串", func(m map[string]any) { m["label"] = true }, "label", "字符串"},
		{"credits 是字符串", func(m map[string]any) { m["credits"] = "10" }, "credits", "整数"},
		{"credits 是小数", func(m map[string]any) { m["credits"] = 1.5 }, "credits", "整数"},
		{"enabled 不是布尔", func(m map[string]any) { m["enabled"] = "yes" }, "enabled", "布尔"},
		{"sort 是字符串", func(m map[string]any) { m["sort"] = "1" }, "sort", "整数"},
		{"deadline 是布尔", func(m map[string]any) { m["deadline"] = true }, "deadline", "字符串"},
		{"字段 required 不是布尔", func(m map[string]any) { mcDig(m, "input_schema.prompt")["required"] = "true" }, "input_schema.prompt.required", "布尔"},
		{"字段 min 不是数字", func(m map[string]any) {
			mcDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "min": "1"}
		}, "input_schema.n.min", "数字"},
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
	m["credits"] = -1
	m["channels"] = []any{
		map[string]any{"channel": "BAD", "upstream_model": ""},
		map[string]any{"channel": "ok", "upstream_model": "x"},
	}
	mcDig(m, "input_schema.prompt")["type"] = "string"
	_, issues := modelcfg.ParseModel(mcMarshal(t, m))
	for _, p := range []string{"key", "kind", "credits", "channels", "channels[0].channel", "channels[0].upstream_model", "input_schema.prompt.type"} {
		if _, ok := mcHasIssue(issues, p); !ok {
			t.Errorf("缺少路径 %s，实际 %v", p, mcIssueList(issues))
		}
	}
}

func TestParseModel_重复字段名(t *testing.T) {
	body := `{"key":"m","kind":"image","label":"x","channels":[{"channel":"c","upstream_model":"u"}],"input_schema":{
		"a":{"type":"text","label":"A"},"a":{"type":"text","label":"B"}}}`
	_, issues := modelcfg.ParseModel([]byte(body))
	if is, ok := mcHasIssue(issues, "input_schema.a"); !ok || !strings.Contains(is.Message, "重复") {
		t.Fatalf("期望重复字段名 Issue，实际：%v", mcIssueList(issues))
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
		{"空 input_schema", func(m map[string]any) { m["input_schema"] = map[string]any{} }},
		{"没有 input_schema", func(m map[string]any) { delete(m, "input_schema") }},
		{"credits 为 0", func(m map[string]any) { m["credits"] = 0 }},
		{"kind=text", func(m map[string]any) {
			m["kind"] = "text"
			m["input_schema"] = map[string]any{"prompt": map[string]any{"type": "text", "label": "提问", "required": true}}
		}},
		{"kind=image", func(m map[string]any) { m["kind"] = "image" }},
		{"kind=audio", func(m map[string]any) { m["kind"] = "audio" }},
		{"key 含点、下划线、大写", func(m map[string]any) { m["key"] = "Kling_v2.master-1" }},
		{"key 恰好 128 位", func(m map[string]any) { m["key"] = strings.Repeat("a", 128) }},
		{"channel 恰好 64 位", func(m map[string]any) { mcChannel0(m)["channel"] = strings.Repeat("a", 64) }},
		{"upstream_model 恰好 128 个字符", func(m map[string]any) { mcChannel0(m)["upstream_model"] = strings.Repeat("模", 128) }},
		{"upstream_model 含斜杠与冒号", func(m map[string]any) { mcChannel0(m)["upstream_model"] = "org/model:v1" }},
		{"字符串枚举", func(m map[string]any) {
			mcDig(m, "input_schema")["ar"] = map[string]any{"type": "enum", "label": "比例", "default": "16:9",
				"options": []any{map[string]any{"value": "16:9", "label": "横屏"}, map[string]any{"value": "9:16", "label": "竖屏"}}}
		}},
		{"boolean 与 number", func(m map[string]any) {
			mcDig(m, "input_schema")["hd"] = map[string]any{"type": "boolean", "label": "高清", "default": false, "advanced": true}
			mcDig(m, "input_schema")["seed"] = map[string]any{"type": "number", "label": "种子", "min": 0, "max": 100, "default": 1}
		}},
		{"字段名以下划线开头", func(m map[string]any) {
			mcDig(m, "input_schema")["_x"] = map[string]any{"type": "text", "label": "X"}
		}},
		{"多个媒体字段", func(m map[string]any) {
			mcDig(m, "input_schema")["tail"] = map[string]any{"type": "image", "label": "尾帧", "port": "image"}
			mcDig(m, "input_schema")["voice"] = map[string]any{"type": "audio", "label": "配音"}
		}},
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

func TestValidateInputSchema(t *testing.T) {
	t.Run("空 schema 合法", func(t *testing.T) {
		if issues := modelcfg.ValidateInputSchema(nil); len(issues) != 0 {
			t.Fatalf("实际 %v", mcIssueList(issues))
		}
	})
	t.Run("Go 里构造的 schema（数字是 int / float64 / json.Number）合法", func(t *testing.T) {
		schema := modelcfg.InputSchema{
			{Name: "duration", InputField: modelcfg.InputField{Type: modelcfg.FieldEnum, Label: "时长", Default: json.Number("5"),
				Options: []modelcfg.EnumOption{{Value: 5, Label: "5 秒"}, {Value: 10.0, Label: "10 秒"}, {Value: json.Number("15"), Label: "15 秒"}}}},
			{Name: "seed", InputField: modelcfg.InputField{Type: modelcfg.FieldNumber, Label: "种子", Default: int64(3)}},
		}
		if issues := modelcfg.ValidateInputSchema(schema); len(issues) != 0 {
			t.Fatalf("实际 %v", mcIssueList(issues))
		}
	})
	t.Run("路径带 input_schema 前缀，多个问题一起返回且保持书写顺序", func(t *testing.T) {
		schema := modelcfg.InputSchema{
			{Name: "b", InputField: modelcfg.InputField{Type: "file", Label: "B"}},
			{Name: "a", InputField: modelcfg.InputField{Type: modelcfg.FieldText, Label: ""}},
			{Name: "a", InputField: modelcfg.InputField{Type: modelcfg.FieldText, Label: "A2"}},
			{Name: "x y", InputField: modelcfg.InputField{Type: modelcfg.FieldText, Label: "X"}},
		}
		issues := modelcfg.ValidateInputSchema(schema)
		got := strings.Join(mcIssuePaths(issues), ",")
		want := "input_schema.b.type,input_schema.a.label,input_schema.a,input_schema.x y"
		if got != want {
			t.Fatalf("路径 %s，期望 %s", got, want)
		}
	})
}
