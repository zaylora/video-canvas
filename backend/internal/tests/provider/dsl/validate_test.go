// 本文件：validate.go、hosts.go、types.go 与 jsonschema.go 的单元测试，使用 testdata 下的设计文档示例夹具。

package dsl_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	. "video-canvas/internal/provider/dsl"
)

// dslLoadFixture 读取 testdata 下的夹具并解码成通用 map，供用例按需修改。
func dslLoadFixture(t *testing.T, name string) map[string]any {
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

func dslMarshal(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// dslDig 取嵌套 map，路径用点分隔。
func dslDig(m map[string]any, path string) map[string]any {
	cur := m
	for _, p := range strings.Split(path, ".") {
		cur = cur[p].(map[string]any)
	}
	return cur
}

func dslHasIssue(issues []Issue, path string) (Issue, bool) {
	for _, i := range issues {
		if i.Path == path {
			return i, true
		}
	}
	return Issue{}, false
}

func dslIssuePaths(issues []Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Path+"："+i.Message)
	}
	return out
}

func TestParseProvider_设计文档示例通过校验(t *testing.T) {
	b, err := os.ReadFile("testdata/runninghub_provider.json")
	if err != nil {
		t.Fatal(err)
	}
	p, issues := ParseProvider(b)
	if len(issues) > 0 {
		t.Fatalf("应通过校验，实际问题：%v", dslIssuePaths(issues))
	}
	if p.Key != "runninghub" || p.Operations.Submit == nil || p.Operations.Cancel != nil {
		t.Fatalf("解析结果不符：%+v", p)
	}
	if p.Poll.FirstDelay.D().Seconds() != 10 || p.Poll.Jitter != 0.2 {
		t.Fatalf("poll 解析错误：%+v", p.Poll)
	}
	if p.Operations.Upload.Encoding.FileField != "file" {
		t.Fatalf("上传编码解析错误：%+v", p.Operations.Upload)
	}
}

func TestParseModel_设计文档示例通过校验(t *testing.T) {
	pb, _ := os.ReadFile("testdata/runninghub_provider.json")
	prov, issues := ParseProvider(pb)
	if len(issues) > 0 {
		t.Fatalf("provider 应通过：%v", dslIssuePaths(issues))
	}
	mb, _ := os.ReadFile("testdata/runninghub_model.json")

	for _, tt := range []struct {
		name string
		prov *ProviderConfig
	}{{"不带 provider", nil}, {"带 provider 跨对象校验", prov}} {
		t.Run(tt.name, func(t *testing.T) {
			m, issues := ParseModel(mb, tt.prov)
			if len(issues) > 0 {
				t.Fatalf("应通过校验，实际问题：%v", dslIssuePaths(issues))
			}
			if m.Key != "rh-2093984571330498561" || m.Credits != 10 || m.Deadline.D().Minutes() != 30 {
				t.Fatalf("解析结果不符：%+v", m)
			}
			// input_schema 保序
			var names []string
			for _, e := range m.InputSchema {
				names = append(names, e.Name)
			}
			if strings.Join(names, ",") != "prompt,image,duration" {
				t.Fatalf("input_schema 顺序错误：%v", names)
			}
			if v := m.Params["webappId"]; v != "2093984571330498561" {
				t.Fatalf("params 错误：%v", m.Params)
			}
		})
	}
}

func TestParseModel_补默认值(t *testing.T) {
	m := dslLoadFixture(t, "runninghub_model.json")
	delete(m, "deadline")
	delete(m, "output")
	got, issues := ParseModel(dslMarshal(t, m), nil)
	if len(issues) > 0 {
		t.Fatalf("应通过：%v", dslIssuePaths(issues))
	}
	if got.Deadline.D() != DefaultModelDeadline || got.Output.Media != "video" || got.Output.Select != "outputs" {
		t.Fatalf("默认值不对：%+v", got)
	}
}

func TestParseModel_大整数参数不丢精度(t *testing.T) {
	m := `{"key":"m","kind":"image","provider":"p","label":"x","params":{"webappId":2093984571330498561,"n":5},
	"input_schema":{"a":{"type":"text","label":"A"}}}`
	got, issues := ParseModel([]byte(m), nil)
	if len(issues) > 0 {
		t.Fatalf("应通过：%v", dslIssuePaths(issues))
	}
	if got.Params["webappId"] != 2093984571330498561 {
		t.Fatalf("大整数丢精度：%#v", got.Params["webappId"])
	}
	if got.Params["n"] != 5.0 {
		t.Fatalf("小整数应为 float64：%#v", got.Params["n"])
	}
}

func TestParseProvider_非法配置的Issue路径(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(m map[string]any)
		wantPath string
		wantMsg  string // 消息里应包含的片段，可空
	}{
		{"dsl 版本不对", func(m map[string]any) { m["dsl"] = 2 }, "dsl", ""},
		{"key 含大写", func(m map[string]any) { m["key"] = "RunningHub" }, "key", ""},
		{"name 为空", func(m map[string]any) { m["name"] = " " }, "name", ""},
		{"base_url 不是 URL", func(m map[string]any) { m["base_url"] = "not a url" }, "base_url", ""},
		{"base_url 域名不在白名单", func(m map[string]any) { m["base_url"] = "https://evil.example.com" }, "base_url", "白名单"},
		{"base_url 是 IP", func(m map[string]any) { m["base_url"] = "http://127.0.0.1:8080" }, "base_url", "IP"},
		{"base_url 协议不对", func(m map[string]any) { m["base_url"] = "ftp://www.runninghub.cn" }, "base_url", ""},
		{"allowed_hosts 为空", func(m map[string]any) { m["allowed_hosts"] = []any{} }, "allowed_hosts", ""},
		{"allowed_hosts 带端口", func(m map[string]any) { m["allowed_hosts"] = []any{"www.runninghub.cn", "a.com:8080"} }, "allowed_hosts[1]", ""},
		{"allowed_hosts 过宽通配", func(m map[string]any) { m["allowed_hosts"] = []any{"www.runninghub.cn", "*.com"} }, "allowed_hosts[1]", "过宽"},
		{"allowed_hosts 是 IP", func(m map[string]any) { m["allowed_hosts"] = []any{"www.runninghub.cn", "10.0.0.1"} }, "allowed_hosts[1]", "IP"},
		{"allowed_hosts 大写", func(m map[string]any) { m["allowed_hosts"] = []any{"www.runninghub.cn", "A.com"} }, "allowed_hosts[1]", "小写"},
		{"auth 类型未知", func(m map[string]any) { dslDig(m, "auth")["type"] = "oauth" }, "auth.type", ""},
		{"bearer 缺 secret", func(m map[string]any) { delete(dslDig(m, "auth"), "secret") }, "auth.secret", ""},
		{"header 缺 name", func(m map[string]any) { m["auth"] = map[string]any{"type": "header", "secret": "k"} }, "auth.name", ""},
		{"query 缺 name", func(m map[string]any) { m["auth"] = map[string]any{"type": "query", "secret": "k"} }, "auth.name", ""},
		{"none 却写了 secret", func(m map[string]any) { m["auth"] = map[string]any{"type": "none", "secret": "k"} }, "auth.secret", ""},
		{"rps 为负", func(m map[string]any) { dslDig(m, "rate_limit")["rps"] = -1 }, "rate_limit.rps", ""},
		{"并发为负", func(m map[string]any) { dslDig(m, "rate_limit")["max_concurrency"] = -1 }, "rate_limit.max_concurrency", ""},
		{"poll 时长格式错误", func(m map[string]any) { dslDig(m, "poll")["interval"] = "abc" }, "poll.interval", "时长格式"},
		{"poll jitter 越界", func(m map[string]any) { dslDig(m, "poll")["jitter"] = 2 }, "poll.jitter", ""},
		{"poll max_interval 小于 interval", func(m map[string]any) { dslDig(m, "poll")["max_interval"] = "1s" }, "poll.max_interval", ""},
		{"缺少 submit", func(m map[string]any) { delete(dslDig(m, "operations"), "submit") }, "operations.submit", ""},
		{"缺少 query", func(m map[string]any) { delete(dslDig(m, "operations"), "query") }, "operations.query", ""},
		{"method 非法", func(m map[string]any) { dslDig(m, "operations.submit")["method"] = "FETCH" }, "operations.submit.method", ""},
		{"path 不以斜杠开头", func(m map[string]any) { dslDig(m, "operations.submit")["path"] = "https://evil.com/x" }, "operations.submit.path", ""},
		{"path 表达式语法错误", func(m map[string]any) { dslDig(m, "operations.submit")["path"] = "/x/${ model.params. }" }, "operations.submit.path", ""},
		{"body 里表达式语法错误", func(m map[string]any) {
			dslDig(m, "operations.submit.body")["webhookUrl"] = "${ ctx.webhook_url + }"
		}, "operations.submit.body.webhookUrl", "表达式"},
		{"body 引用不可用的变量 resp", func(m map[string]any) {
			dslDig(m, "operations.submit.body")["x"] = "${ resp.a }"
		}, "operations.submit.body.x", "resp"},
		{"body 引用未知变量 secret", func(m map[string]any) {
			dslDig(m, "operations.submit.body")["x"] = "${ secret }"
		}, "operations.submit.body.x", "secret"},
		{"body 表达式缺少结束括号", func(m map[string]any) {
			dslDig(m, "operations.submit.body")["x"] = "${ input.a"
		}, "operations.submit.body.x", "}"},
		{"header 值表达式错误", func(m map[string]any) {
			dslDig(m, "operations.submit")["headers"] = map[string]any{"X-A": "${ 1 + }"}
		}, "operations.submit.headers.X-A", ""},
		{"header 名非法", func(m map[string]any) {
			dslDig(m, "operations.submit")["headers"] = map[string]any{"Bad Name": "x"}
		}, "operations.submit.headers.Bad Name", ""},
		{"禁止自定义 Host", func(m map[string]any) {
			dslDig(m, "operations.submit")["headers"] = map[string]any{"Host": "x"}
		}, "operations.submit.headers.Host", ""},
		{"query 参数表达式错误", func(m map[string]any) {
			dslDig(m, "operations.query")["query"] = map[string]any{"a": "${ 1 + }"}
		}, "operations.query.query.a", ""},
		{"encoding 未知", func(m map[string]any) { dslDig(m, "operations.submit.encoding")["type"] = "xml" }, "operations.submit.encoding.type", ""},
		{"上传必须 multipart", func(m map[string]any) { dslDig(m, "operations.upload.encoding")["type"] = "json" }, "operations.upload.encoding.type", ""},
		{"上传缺 file_field", func(m map[string]any) { delete(dslDig(m, "operations.upload.encoding"), "file_field") }, "operations.upload.encoding.file_field", ""},
		{"GET 不能有 body", func(m map[string]any) { dslDig(m, "operations.query")["method"] = "GET" }, "operations.query.body", ""},
		{"success 为空", func(m map[string]any) { dslDig(m, "operations.submit")["success"] = "" }, "operations.submit.success", ""},
		{"success 语法错误", func(m map[string]any) { dslDig(m, "operations.submit")["success"] = "status ==" }, "operations.submit.success", ""},
		{"success 引用未知变量", func(m map[string]any) { dslDig(m, "operations.query")["success"] = "foo == 1" }, "operations.query.success", "foo"},
		{"extract 缺 provider_task_id", func(m map[string]any) { dslDig(m, "operations.submit")["extract"] = map[string]any{} }, "operations.submit.extract.provider_task_id", ""},
		{"extract 未知键", func(m map[string]any) {
			dslDig(m, "operations.submit.extract")["typo"] = "resp.x"
		}, "operations.submit.extract.typo", "未知"},
		{"extract 表达式错误", func(m map[string]any) {
			dslDig(m, "operations.query.extract")["outputs"] = "map(resp.results, {"
		}, "operations.query.extract.outputs", ""},
		{"query 缺 status", func(m map[string]any) { delete(dslDig(m, "operations.query.extract"), "status") }, "operations.query.extract.status", ""},
		{"upload 缺 ref", func(m map[string]any) { dslDig(m, "operations.upload")["extract"] = map[string]any{} }, "operations.upload.extract.ref", ""},
		{"超时过大", func(m map[string]any) { dslDig(m, "operations.submit")["timeout"] = "1h" }, "operations.submit.timeout", ""},
		{"超时格式错误", func(m map[string]any) { dslDig(m, "operations.submit")["timeout"] = "xx" }, "operations.submit.timeout", "时长格式"},
		{"status_map 值非法", func(m map[string]any) { dslDig(m, "status_map")["DONE"] = "finished" }, "status_map.DONE", ""},
		{"status_map 没有 succeeded", func(m map[string]any) {
			m["status_map"] = map[string]any{"FAILED": "failed", "RUNNING": "running"}
		}, "status_map", "succeeded"},
		{"status_map 没有 failed", func(m map[string]any) {
			m["status_map"] = map[string]any{"OK": "succeeded"}
		}, "status_map", "failed"},
		{"error_rules when 语法错误", func(m map[string]any) {
			m["error_rules"] = []any{
				map[string]any{"when": "true", "class": "terminal"},
				map[string]any{"when": "status ==", "class": "terminal"},
			}
		}, "error_rules[1].when", ""},
		{"error_rules class 非法", func(m map[string]any) {
			m["error_rules"] = []any{map[string]any{"when": "true", "class": "fatal"}}
		}, "error_rules[0].class", ""},
		{"error_rules when 为空", func(m map[string]any) {
			m["error_rules"] = []any{map[string]any{"when": "", "class": "terminal"}}
		}, "error_rules[0].when", ""},
		{"webhook verify 未知", func(m map[string]any) { dslDig(m, "webhook.verify")["type"] = "hmac" }, "webhook.verify.type", ""},
		{"webhook task_id 用了 resp", func(m map[string]any) { dslDig(m, "webhook")["task_id"] = "resp.taskId" }, "webhook.task_id", "resp"},
		{"未知顶层字段", func(m map[string]any) { m["extra"] = 1 }, "extra", "未知字段"},
		{"未知嵌套字段", func(m map[string]any) { dslDig(m, "operations.submit")["mehtod"] = "POST" }, "operations.submit.mehtod", "未知字段"},
		{"类型错误：name 是数字", func(m map[string]any) { m["name"] = 5 }, "name", "字符串"},
		{"类型错误：allowed_hosts 元素是数字", func(m map[string]any) { m["allowed_hosts"] = []any{"a.com", 1} }, "allowed_hosts[1]", "字符串"},
		{"类型错误：rps 是字符串", func(m map[string]any) { dslDig(m, "rate_limit")["rps"] = "5" }, "rate_limit.rps", "数字"},
		{"类型错误：error_rules 不是数组", func(m map[string]any) { m["error_rules"] = map[string]any{} }, "error_rules", "数组"},
		{"类型错误：operations.submit 是字符串", func(m map[string]any) { dslDig(m, "operations")["submit"] = "x" }, "operations.submit", "对象"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := dslLoadFixture(t, "runninghub_provider.json")
			tt.mutate(m)
			got, issues := ParseProvider(dslMarshal(t, m))
			if got != nil || len(issues) == 0 {
				t.Fatalf("期望校验失败")
			}
			is, ok := dslHasIssue(issues, tt.wantPath)
			if !ok {
				t.Fatalf("期望 Issue 路径 %q，实际：%v", tt.wantPath, dslIssuePaths(issues))
			}
			if tt.wantMsg != "" && !strings.Contains(is.Message, tt.wantMsg) {
				t.Fatalf("Issue 消息 %q 应包含 %q", is.Message, tt.wantMsg)
			}
		})
	}
}

func TestParseProvider_JSON语法与结构错误(t *testing.T) {
	tests := []struct {
		name, body string
	}{
		{"空正文", ""},
		{"非法 JSON", `{"dsl": 1,`},
		{"数组", `[]`},
		{"多余内容", `{} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, issues := ParseProvider([]byte(tt.body))
			if p != nil || len(issues) != 1 || issues[0].Path != "" {
				t.Fatalf("期望单条根路径 Issue，实际 %v", dslIssuePaths(issues))
			}
		})
	}
}

func TestParseProvider_多个问题一次报出(t *testing.T) {
	m := dslLoadFixture(t, "runninghub_provider.json")
	dslDig(m, "operations.submit")["method"] = "FETCH"
	dslDig(m, "operations.submit.body")["x"] = "${ 1 + }"
	m["error_rules"] = []any{map[string]any{"when": "status ==", "class": "terminal"}}
	_, issues := ParseProvider(dslMarshal(t, m))
	for _, p := range []string{"operations.submit.method", "operations.submit.body.x", "error_rules[0].when"} {
		if _, ok := dslHasIssue(issues, p); !ok {
			t.Errorf("缺少路径 %s，实际 %v", p, dslIssuePaths(issues))
		}
	}
}

func TestParseProvider_合法变体(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"header 鉴权", func(m map[string]any) {
			m["auth"] = map[string]any{"type": "header", "secret": "k", "name": "X-Api-Key"}
		}},
		{"query 鉴权", func(m map[string]any) { m["auth"] = map[string]any{"type": "query", "secret": "k", "name": "key"} }},
		{"body_field 鉴权", func(m map[string]any) {
			m["auth"] = map[string]any{"type": "body_field", "secret": "k", "name": "apiKey"}
		}},
		{"none 鉴权", func(m map[string]any) { m["auth"] = map[string]any{"type": "none"} }},
		{"没有 upload 与 webhook", func(m map[string]any) {
			delete(dslDig(m, "operations"), "upload")
			delete(m, "webhook")
		}},
		{"method 小写自动规范", func(m map[string]any) { dslDig(m, "operations.submit")["method"] = "post" }},
		{"有 cancel 操作", func(m map[string]any) {
			dslDig(m, "operations")["cancel"] = map[string]any{"method": "POST", "path": "/cancel", "body": map[string]any{"taskId": "${ task.provider_task_id }"}}
		}},
		{"encoding 缺省为 json", func(m map[string]any) { delete(dslDig(m, "operations.submit"), "encoding") }},
		{"时长写成数字（秒）", func(m map[string]any) { dslDig(m, "poll")["interval"] = 5 }},
		{"body 是数组", func(m map[string]any) {
			dslDig(m, "operations")["submit"].(map[string]any)["body"] = []any{"${ input.a }"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := dslLoadFixture(t, "runninghub_provider.json")
			tt.mutate(m)
			p, issues := ParseProvider(dslMarshal(t, m))
			if len(issues) > 0 {
				t.Fatalf("应通过：%v", dslIssuePaths(issues))
			}
			if tt.name == "method 小写自动规范" && p.Operations.Submit.Method != "POST" {
				t.Fatalf("method 未规范化：%s", p.Operations.Submit.Method)
			}
			if tt.name == "encoding 缺省为 json" && p.Operations.Submit.Encoding.Type != EncodingJSON {
				t.Fatalf("encoding 未补默认：%+v", p.Operations.Submit.Encoding)
			}
		})
	}
}

func TestParseModel_非法配置的Issue路径(t *testing.T) {
	pb, _ := os.ReadFile("testdata/runninghub_provider.json")
	prov, _ := ParseProvider(pb)

	tests := []struct {
		name     string
		mutate   func(m map[string]any)
		prov     *ProviderConfig
		wantPath string
		wantMsg  string
	}{
		{"key 非法", func(m map[string]any) { m["key"] = "a b" }, nil, "key", ""},
		{"kind 非法", func(m map[string]any) { m["kind"] = "text" }, nil, "kind", ""},
		{"provider 为空", func(m map[string]any) { m["provider"] = "" }, nil, "provider", ""},
		{"label 为空", func(m map[string]any) { m["label"] = "" }, nil, "label", ""},
		{"credits 为负", func(m map[string]any) { m["credits"] = -1 }, nil, "credits", ""},
		{"deadline 格式错误", func(m map[string]any) { m["deadline"] = "soon" }, nil, "deadline", "时长格式"},
		{"deadline 过长", func(m map[string]any) { m["deadline"] = "48h" }, nil, "deadline", ""},
		{"output.media 非法", func(m map[string]any) { dslDig(m, "output")["media"] = "gif" }, nil, "output.media", ""},
		{"output.select 语法错误", func(m map[string]any) { dslDig(m, "output")["select"] = "filter(outputs, " }, nil, "output.select", ""},
		{"output.select 用了 resp", func(m map[string]any) { dslDig(m, "output")["select"] = "resp.results" }, nil, "output.select", "resp"},
		{"mapping 表达式语法错误", func(m map[string]any) {
			dslDig(m, "mapping")["nodeInfoList"].([]any)[2].(map[string]any)["fieldValue"] = "${ input.duration * }"
		}, nil, "mapping.nodeInfoList[2].fieldValue", ""},
		{"mapping 引用不存在的 input 字段", func(m map[string]any) {
			dslDig(m, "mapping")["nodeInfoList"].([]any)[0].(map[string]any)["fieldValue"] = "${ input.promt }"
		}, nil, "mapping.nodeInfoList[0].fieldValue", "input.promt"},
		{"mapping 的 files 引用非媒体字段", func(m map[string]any) {
			dslDig(m, "mapping")["nodeInfoList"].([]any)[1].(map[string]any)["fieldValue"] = "${ files.prompt }"
		}, nil, "mapping.nodeInfoList[1].fieldValue", "媒体字段"},
		{"mapping 的 files 引用不存在的字段", func(m map[string]any) {
			dslDig(m, "mapping")["nodeInfoList"].([]any)[1].(map[string]any)["fieldValue"] = "${ files.first }"
		}, nil, "mapping.nodeInfoList[1].fieldValue", "files.first"},
		{"mapping 用了 resp", func(m map[string]any) {
			dslDig(m, "mapping")["nodeInfoList"].([]any)[0].(map[string]any)["fieldValue"] = "${ resp.x }"
		}, nil, "mapping.nodeInfoList[0].fieldValue", "resp"},
		{"input_schema 字段类型非法", func(m map[string]any) { dslDig(m, "input_schema.prompt")["type"] = "string" }, nil, "input_schema.prompt.type", ""},
		{"input_schema 缺 label", func(m map[string]any) { dslDig(m, "input_schema.prompt")["label"] = "" }, nil, "input_schema.prompt.label", ""},
		{"input_schema 字段名不合法", func(m map[string]any) {
			dslDig(m, "input_schema")["first-frame"] = map[string]any{"type": "image", "label": "x"}
		}, nil, "input_schema.first-frame", ""},
		{"enum 没有 options", func(m map[string]any) { delete(dslDig(m, "input_schema.duration"), "options") }, nil, "input_schema.duration.options", ""},
		{"enum 选项 value 类型错误", func(m map[string]any) {
			dslDig(m, "input_schema.duration")["options"] = []any{map[string]any{"value": true, "label": "x"}}
		}, nil, "input_schema.duration.options[0].value", ""},
		{"enum 选项重复", func(m map[string]any) {
			dslDig(m, "input_schema.duration")["options"] = []any{
				map[string]any{"value": 5, "label": "5 秒"}, map[string]any{"value": 5.0, "label": "又 5 秒"},
			}
		}, nil, "input_schema.duration.options[1].value", "重复"},
		{"enum 选项缺 label", func(m map[string]any) {
			dslDig(m, "input_schema.duration")["options"] = []any{map[string]any{"value": 5, "label": ""}}
			delete(dslDig(m, "input_schema.duration"), "default")
		}, nil, "input_schema.duration.options[0].label", ""},
		{"enum 默认值不在选项内", func(m map[string]any) { dslDig(m, "input_schema.duration")["default"] = 7 }, nil, "input_schema.duration.default", ""},
		{"非 enum 写了 options", func(m map[string]any) {
			dslDig(m, "input_schema.prompt")["options"] = []any{map[string]any{"value": "a", "label": "a"}}
		}, nil, "input_schema.prompt.options", ""},
		{"number min 大于 max", func(m map[string]any) {
			dslDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "min": 10, "max": 1}
		}, nil, "input_schema.n.min", ""},
		{"text 写了 min", func(m map[string]any) { dslDig(m, "input_schema.prompt")["min"] = 1 }, nil, "input_schema.prompt.min", ""},
		{"number 写了 max_length", func(m map[string]any) {
			dslDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "max_length": 5}
		}, nil, "input_schema.n.max_length", ""},
		{"port 非法", func(m map[string]any) { dslDig(m, "input_schema.prompt")["port"] = "file" }, nil, "input_schema.prompt.port", ""},
		{"媒体字段 port 与类型不符", func(m map[string]any) { dslDig(m, "input_schema.image")["port"] = "video" }, nil, "input_schema.image.port", ""},
		{"number 字段不能有 port", func(m map[string]any) {
			dslDig(m, "input_schema")["n"] = map[string]any{"type": "number", "label": "N", "port": "text"}
		}, nil, "input_schema.n.port", ""},
		{"媒体字段不能有默认值", func(m map[string]any) { dslDig(m, "input_schema.image")["default"] = 1 }, nil, "input_schema.image.default", ""},
		{"text 默认值类型错误", func(m map[string]any) { dslDig(m, "input_schema.prompt")["default"] = 5 }, nil, "input_schema.prompt.default", ""},
		{"未知字段", func(m map[string]any) { m["mapp"] = 1 }, nil, "mapp", "未知字段"},
		{"input_schema 字段里的未知属性", func(m map[string]any) { dslDig(m, "input_schema.prompt")["requried"] = true }, nil, "input_schema.prompt.requried", "未知字段"},
		{"input_schema 不是对象", func(m map[string]any) { m["input_schema"] = []any{} }, nil, "input_schema", "对象"},
		{"Model.Provider 与 provider.Key 不一致", func(m map[string]any) { m["provider"] = "other" }, prov, "provider", ""},
		{"provider 没有 submit", func(m map[string]any) {}, &ProviderConfig{Key: "runninghub", Operations: Operations{Query: &Operation{}}}, "provider", "submit"},
		{"provider 没有 query", func(m map[string]any) {}, &ProviderConfig{Key: "runninghub", Operations: Operations{Submit: &Operation{}}}, "provider", "query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := dslLoadFixture(t, "runninghub_model.json")
			tt.mutate(m)
			got, issues := ParseModel(dslMarshal(t, m), tt.prov)
			if got != nil || len(issues) == 0 {
				t.Fatalf("期望校验失败")
			}
			is, ok := dslHasIssue(issues, tt.wantPath)
			if !ok {
				t.Fatalf("期望 Issue 路径 %q，实际：%v", tt.wantPath, dslIssuePaths(issues))
			}
			if tt.wantMsg != "" && !strings.Contains(is.Message, tt.wantMsg) {
				t.Fatalf("Issue 消息 %q 应包含 %q", is.Message, tt.wantMsg)
			}
		})
	}
}

func TestParseModel_平台模板引用非媒体字段的files(t *testing.T) {
	prov := &ProviderConfig{
		Key: "p",
		Operations: Operations{
			Submit: &Operation{Method: "POST", Path: "/x", Body: map[string]any{"img": "${ files.prompt }"}},
			Query:  &Operation{Method: "POST", Path: "/q"},
		},
	}
	mb, _ := os.ReadFile("testdata/runninghub_model.json")
	var m map[string]any
	_ = json.Unmarshal(mb, &m)
	m["provider"] = "p"
	_, issues := ParseModel(dslMarshal(t, m), prov)
	if _, ok := dslHasIssue(issues, "provider.operations.submit.body.img"); !ok {
		t.Fatalf("期望平台模板引用被拦截，实际：%v", dslIssuePaths(issues))
	}
}

func TestParseModel_合法变体(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"没有 mapping", func(m map[string]any) { delete(m, "mapping") }},
		{"没有 params", func(m map[string]any) { delete(m, "params") }},
		{"空 input_schema", func(m map[string]any) {
			m["input_schema"] = map[string]any{}
			delete(m, "mapping")
		}},
		{"字符串枚举", func(m map[string]any) {
			dslDig(m, "input_schema")["ar"] = map[string]any{"type": "enum", "label": "比例", "default": "16:9",
				"options": []any{map[string]any{"value": "16:9", "label": "横屏"}, map[string]any{"value": "9:16", "label": "竖屏"}}}
		}},
		{"boolean 与 number", func(m map[string]any) {
			dslDig(m, "input_schema")["hd"] = map[string]any{"type": "boolean", "label": "高清", "default": false, "advanced": true}
			dslDig(m, "input_schema")["seed"] = map[string]any{"type": "number", "label": "种子", "min": 0, "max": 100, "default": 1}
		}},
		{"mapping 使用 model.params 与 ctx", func(m map[string]any) {
			m["mapping"] = map[string]any{"a": "${ model.params.webappId }-${ ctx.now }"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := dslLoadFixture(t, "runninghub_model.json")
			tt.mutate(m)
			if _, issues := ParseModel(dslMarshal(t, m), nil); len(issues) > 0 {
				t.Fatalf("应通过：%v", dslIssuePaths(issues))
			}
		})
	}
}

func TestParseModel_重复字段名(t *testing.T) {
	body := `{"key":"m","kind":"image","provider":"p","label":"x","input_schema":{
		"a":{"type":"text","label":"A"},"a":{"type":"text","label":"B"}}}`
	_, issues := ParseModel([]byte(body), nil)
	if is, ok := dslHasIssue(issues, "input_schema.a"); !ok || !strings.Contains(is.Message, "重复") {
		t.Fatalf("期望重复字段名 Issue，实际：%v", dslIssuePaths(issues))
	}
}

func TestValidateHostPattern(t *testing.T) {
	tests := []struct {
		pattern string
		wantErr string // 空表示应该通过；否则是错误信息里应包含的片段
	}{
		{"www.runninghub.cn", ""},
		{"*.runninghub.cn", ""},
		{"*.aliyuncs.com", ""},
		{"*.oss-cn-hangzhou.aliyuncs.com", ""},
		{"*.cloudfront.net", ""},   // 私有后缀：云厂商托管结果文件常用，允许
		{"*.s3.amazonaws.com", ""}, // 同上
		{"*.example.com.cn", ""},
		{"*.com", "过宽"},
		{"*.com.cn", "公共后缀"},
		{"*.co.uk", "公共后缀"},
		{"com.cn", ""}, // 非通配的精确域名不受影响
		{"*.internal", "过宽"},
		{"", "不能为空"},
		{"A.com", "小写"},
		{"a.com:8080", "端口"},
		{"https://a.com", "协议"},
		{"10.0.0.1", "IP"},
		{"*.10.0.0.1", "IP"},
		{"a_b.com", "格式"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			err := ValidateHostPattern(tt.pattern)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过，实际：%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("期望错误包含 %q，实际：%v", tt.wantErr, err)
			}
		})
	}
}

func TestMatchHost(t *testing.T) {
	patterns := []string{"www.runninghub.cn", "*.runninghub.ai"}
	tests := []struct {
		host string
		want bool
	}{
		{"www.runninghub.cn", true},
		{"WWW.RunningHub.CN", true},
		{"www.runninghub.cn.", true},
		{"runninghub.cn", false},
		{"evil-www.runninghub.cn", false},
		{"a.runninghub.ai", true},
		{"a.b.runninghub.ai", true},
		{"runninghub.ai", false},
		{"xrunninghub.ai", false},
		{"", false},
		{"127.0.0.1", false},
	}
	for _, tt := range tests {
		if got := MatchHost(patterns, tt.host); got != tt.want {
			t.Errorf("MatchHost(%q) = %v, 期望 %v", tt.host, got, tt.want)
		}
	}
}

func TestInputSchema_保序编解码(t *testing.T) {
	src := `{"z":{"type":"text","label":"Z"},"a":{"type":"number","label":"A","min":1},"m":{"type":"enum","label":"M","options":[{"value":1,"label":"一"}]}}`
	var s InputSchema
	if err := json.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 || s[0].Name != "z" || s[1].Name != "a" || s[2].Name != "m" {
		t.Fatalf("顺序不对：%+v", s)
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), `{"z":`) || strings.Index(string(out), `"a":`) > strings.Index(string(out), `"m":`) {
		t.Fatalf("输出顺序不对：%s", out)
	}
	var back InputSchema
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 || back[1].Min == nil || *back[1].Min != 1 {
		t.Fatalf("往返丢失信息：%+v", back)
	}
	// null 与空对象
	var empty InputSchema
	if err := json.Unmarshal([]byte(`null`), &empty); err != nil || empty != nil {
		t.Fatalf("null 应得到空 schema：%v %v", empty, err)
	}
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil || len(empty) != 0 {
		t.Fatalf("空对象应得到空 schema：%v %v", empty, err)
	}
	if err := json.Unmarshal([]byte(`[]`), &empty); err == nil {
		t.Fatal("数组应报错")
	}
}

func TestJSONSchema(t *testing.T) {
	for _, target := range []string{"provider", "model"} {
		b := JSONSchema(target)
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s schema 不是合法 JSON：%v", target, err)
		}
		props, _ := m["properties"].(map[string]any)
		if len(props) == 0 {
			t.Fatalf("%s schema 没有 properties", target)
		}
	}
	// 枚举要出现在 schema 里
	if !strings.Contains(string(JSONSchema("provider")), "body_field") || !strings.Contains(string(JSONSchema("provider")), "provider_balance") {
		t.Fatal("provider schema 缺少枚举")
	}
	if !strings.Contains(string(JSONSchema("model")), "\"boolean\"") {
		t.Fatal("model schema 缺少字段类型枚举")
	}
	if JSONSchema("other") != nil {
		t.Fatal("未知 target 应返回 nil")
	}
}
