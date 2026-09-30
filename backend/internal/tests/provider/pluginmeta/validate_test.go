package pluginmeta_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginproto"
)

// baseMeta 是一份能通过预检的 meta，各用例在它上面改一处。
func baseMeta() map[string]any {
	return map[string]any{
		"apiVersion":   1,
		"key":          "newapi",
		"name":         "New API",
		"version":      "1.0.0",
		"description":  "自建 New API 网关",
		"auth":         map[string]any{"type": "bearer"},
		"allowedHosts": []any{"cdn.example.com", "*.example.org"},
		"endpoints": map[string]any{
			"text":  map[string]any{"mode": "sync"},
			"video": map[string]any{"mode": "async"},
		},
		"channelSettings": map[string]any{
			"region": map[string]any{"type": "enum", "label": "区域", "options": []any{"cn", "global"}, "default": "cn"},
			"debug":  map[string]any{"type": "boolean", "label": "调试", "default": false},
		},
		"import": map[string]any{"args": map[string]any{
			"webappId": map[string]any{"type": "string", "label": "工作流 ID", "required": true},
		}},
		"poll": map[string]any{"firstDelay": 10, "interval": 5, "maxInterval": 15, "jitter": 0.2},
	}
}

// fullHooks 是实现了全部钩子的导出列表。
func fullHooks() []string { return append([]string(nil), pluginproto.AllHooks...) }

func without(hooks []string, drop ...string) []string {
	var out []string
	for _, h := range hooks {
		keep := true
		for _, d := range drop {
			if h == d {
				keep = false
			}
		}
		if keep {
			out = append(out, h)
		}
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("编码 meta 失败：%v", err)
	}
	return b
}

func hasPath(issues []modelcfg.Issue, path string) bool {
	for _, is := range issues {
		if is.Path == path {
			return true
		}
	}
	return false
}

func TestValidate_Rules(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(m map[string]any)
		hooks     []string
		wantPaths []string // 为空表示期望通过
	}{
		{"合法 meta 通过", nil, nil, nil},
		{"只有 sync endpoint 不需要 query 钩子", func(m map[string]any) {
			m["endpoints"] = map[string]any{"text": map[string]any{"mode": "sync"}}
		}, without(fullHooks(), pluginproto.HookBuildQueryRequest, pluginproto.HookParseQueryResponse), nil},
		{"可选字段全部省略也能通过", func(m map[string]any) {
			for _, k := range []string{"description", "allowedHosts", "channelSettings", "import", "poll"} {
				delete(m, k)
			}
		}, []string{pluginproto.HookBuildSubmitRequest, pluginproto.HookParseSubmitResponse,
			pluginproto.HookBuildQueryRequest, pluginproto.HookParseQueryResponse}, nil},
		{"预发布版本号合法", func(m map[string]any) { m["version"] = "2.0.0-rc.1" }, nil, nil},
		{"多余的导出函数名不拒绝", nil, append(fullHooks(), "helper"), nil},

		{"缺少 apiVersion", func(m map[string]any) { delete(m, "apiVersion") }, nil, []string{"meta.apiVersion"}},
		{"apiVersion 不支持", func(m map[string]any) { m["apiVersion"] = 2 }, nil, []string{"meta.apiVersion"}},
		{"apiVersion 类型不对", func(m map[string]any) { m["apiVersion"] = "1" }, nil, []string{"meta.apiVersion"}},
		{"key 含大写", func(m map[string]any) { m["key"] = "NewAPI" }, nil, []string{"meta.key"}},
		{"key 以连字符开头", func(m map[string]any) { m["key"] = "-api" }, nil, []string{"meta.key"}},
		{"key 超过 30 字符", func(m map[string]any) { m["key"] = strings.Repeat("a", 31) }, nil, []string{"meta.key"}},
		{"缺少 key", func(m map[string]any) { delete(m, "key") }, nil, []string{"meta.key"}},
		{"name 为空白", func(m map[string]any) { m["name"] = "  " }, nil, []string{"meta.name"}},
		{"name 过长", func(m map[string]any) { m["name"] = strings.Repeat("名", 129) }, nil, []string{"meta.name"}},
		{"version 不是 semver", func(m map[string]any) { m["version"] = "1.0" }, nil, []string{"meta.version"}},
		{"version 带 v 前缀", func(m map[string]any) { m["version"] = "v1.0.0" }, nil, []string{"meta.version"}},
		{"version 过长", func(m map[string]any) { m["version"] = "1.0.0-" + strings.Repeat("a", 30) }, nil, []string{"meta.version"}},
		{"description 类型不对", func(m map[string]any) { m["description"] = 1 }, nil, []string{"meta.description"}},

		{"缺少 auth", func(m map[string]any) { delete(m, "auth") }, nil, []string{"meta.auth"}},
		{"auth 不是对象", func(m map[string]any) { m["auth"] = "bearer" }, nil, []string{"meta.auth"}},
		{"auth.type 不是枚举值", func(m map[string]any) { m["auth"] = map[string]any{"type": "basic"} }, nil, []string{"meta.auth.type"}},
		{"header 缺 name", func(m map[string]any) { m["auth"] = map[string]any{"type": "header"} }, nil, []string{"meta.auth.name"}},
		{"header name 含空格", func(m map[string]any) {
			m["auth"] = map[string]any{"type": "header", "name": "X Api Key"}
		}, nil, []string{"meta.auth.name"}},
		{"header name 合法", func(m map[string]any) {
			m["auth"] = map[string]any{"type": "header", "name": "X-Api-Key"}
		}, nil, nil},
		{"query 缺 name", func(m map[string]any) { m["auth"] = map[string]any{"type": "query"} }, nil, []string{"meta.auth.name"}},
		{"custom 合法", func(m map[string]any) { m["auth"] = map[string]any{"type": "custom"} }, nil, nil},

		{"缺少 endpoints", func(m map[string]any) { delete(m, "endpoints") }, nil, []string{"meta.endpoints"}},
		{"endpoints 为空对象", func(m map[string]any) { m["endpoints"] = map[string]any{} }, nil, []string{"meta.endpoints"}},
		{"endpoints 不是对象", func(m map[string]any) { m["endpoints"] = []any{"text"} }, nil, []string{"meta.endpoints"}},
		{"未知 kind", func(m map[string]any) {
			m["endpoints"] = map[string]any{"music": map[string]any{"mode": "sync"}}
		}, nil, []string{"meta.endpoints.music"}},
		{"mode 非法", func(m map[string]any) {
			m["endpoints"] = map[string]any{"video": map[string]any{"mode": "stream"}}
		}, nil, []string{"meta.endpoints.video.mode"}},
		{"缺少 mode", func(m map[string]any) {
			m["endpoints"] = map[string]any{"video": map[string]any{}}
		}, nil, []string{"meta.endpoints.video.mode"}},
		{"endpoint 不是对象", func(m map[string]any) {
			m["endpoints"] = map[string]any{"video": "async"}
		}, nil, []string{"meta.endpoints.video"}},
		{"async 缺 query 钩子", nil, without(fullHooks(), pluginproto.HookBuildQueryRequest),
			[]string{"meta.endpoints.video.mode"}},
		{"async 缺 parseQueryResponse", nil, without(fullHooks(), pluginproto.HookParseQueryResponse),
			[]string{"meta.endpoints.video.mode"}},
		{"缺提交钩子", nil, without(fullHooks(), pluginproto.HookBuildSubmitRequest, pluginproto.HookParseSubmitResponse),
			[]string{"exports.buildSubmitRequest", "exports.parseSubmitResponse"}},

		{"allowedHosts 含 IP", func(m map[string]any) { m["allowedHosts"] = []any{"10.0.0.1"} }, nil, []string{"meta.allowedHosts[0]"}},
		{"allowedHosts 过宽通配", func(m map[string]any) { m["allowedHosts"] = []any{"ok.com", "*.com"} }, nil, []string{"meta.allowedHosts[1]"}},
		{"allowedHosts 中段通配", func(m map[string]any) { m["allowedHosts"] = []any{"a.*.com"} }, nil, []string{"meta.allowedHosts[0]"}},
		{"allowedHosts 带端口", func(m map[string]any) { m["allowedHosts"] = []any{"a.com:443"} }, nil, []string{"meta.allowedHosts[0]"}},
		{"allowedHosts 项不是字符串", func(m map[string]any) { m["allowedHosts"] = []any{1} }, nil, []string{"meta.allowedHosts[0]"}},
		{"allowedHosts 不是数组", func(m map[string]any) { m["allowedHosts"] = "a.com" }, nil, []string{"meta.allowedHosts"}},

		{"设置项 type 非法", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"x": map[string]any{"type": "date", "label": "X"}}
		}, nil, []string{"meta.channelSettings.x.type"}},
		{"设置项缺 label", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"x": map[string]any{"type": "string"}}
		}, nil, []string{"meta.channelSettings.x.label"}},
		{"enum 缺 options", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"x": map[string]any{"type": "enum", "label": "X"}}
		}, nil, []string{"meta.channelSettings.x.options"}},
		{"enum options 重复", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"x": map[string]any{"type": "enum", "label": "X", "options": []any{"a", "a"}}}
		}, nil, []string{"meta.channelSettings.x.options"}},
		{"enum default 不在 options", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"x": map[string]any{"type": "enum", "label": "X", "options": []any{"a"}, "default": "b"}}
		}, nil, []string{"meta.channelSettings.x.default"}},
		{"number default 类型不对", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"x": map[string]any{"type": "number", "label": "X", "default": "1"}}
		}, nil, []string{"meta.channelSettings.x.default"}},
		{"boolean default 类型不对", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"x": map[string]any{"type": "boolean", "label": "X", "default": 1}}
		}, nil, []string{"meta.channelSettings.x.default"}},
		{"设置项名字非法", func(m map[string]any) {
			m["channelSettings"] = map[string]any{"a-b": map[string]any{"type": "string", "label": "X"}}
		}, nil, []string{"meta.channelSettings.a-b"}},
		{"channelSettings 不是对象", func(m map[string]any) { m["channelSettings"] = []any{} }, nil, []string{"meta.channelSettings"}},
		{"import.args 设置项非法", func(m map[string]any) {
			m["import"] = map[string]any{"args": map[string]any{"id": map[string]any{"type": "string"}}}
		}, nil, []string{"meta.import.args.id.label"}},
		{"import 不是对象", func(m map[string]any) { m["import"] = true }, nil, []string{"meta.import"}},

		{"poll 负数", func(m map[string]any) { m["poll"] = map[string]any{"interval": -1} }, nil, []string{"meta.poll.interval"}},
		{"poll jitter 大于 1", func(m map[string]any) { m["poll"] = map[string]any{"jitter": 1.5} }, nil, []string{"meta.poll.jitter"}},
		{"poll 字段类型不对", func(m map[string]any) { m["poll"] = map[string]any{"firstDelay": "10"} }, nil, []string{"meta.poll.firstDelay"}},

		{"有 buildPrepareRequests 缺 parsePrepareResponses", nil, without(fullHooks(), pluginproto.HookParsePrepareResponse),
			[]string{"exports.parsePrepareResponses"}},
		{"只有 parsePrepareResponses 不拒绝", nil, without(fullHooks(), pluginproto.HookBuildPrepareRequests), nil},
		{"导入钩子只有 build", nil, without(fullHooks(), pluginproto.HookParseImportResponse),
			[]string{"exports.parseImportResponse"}},
		{"导入钩子只有 parse", nil, without(fullHooks(), pluginproto.HookBuildImportRequest),
			[]string{"exports.buildImportRequest"}},

		{"meta 超过 64KB", func(m map[string]any) { m["description"] = strings.Repeat("x", 65<<10) }, nil, []string{"meta"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := baseMeta()
			if tt.mutate != nil {
				tt.mutate(m)
			}
			hooks := tt.hooks
			if hooks == nil {
				hooks = fullHooks()
			}
			got, issues := Validate(mustJSON(t, m), hooks)
			if len(tt.wantPaths) == 0 {
				if len(issues) != 0 {
					t.Fatalf("期望通过，实际问题：%+v", issues)
				}
				if got == nil {
					t.Fatal("通过时应返回 Meta")
				}
				return
			}
			for _, p := range tt.wantPaths {
				if !hasPath(issues, p) {
					t.Fatalf("期望有路径 %q 的问题，实际：%+v", p, issues)
				}
			}
			for _, is := range issues {
				if is.Message == "" {
					t.Fatalf("问题缺少说明：%+v", is)
				}
			}
		})
	}
}

func TestValidate_NotObject(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `"meta"`, "1", "{"} {
		got, issues := Validate([]byte(raw), fullHooks())
		if got != nil || !hasPath(issues, "meta") {
			t.Fatalf("%q：期望 meta 路径的问题且不返回 Meta，实际 %v %+v", raw, got, issues)
		}
	}
}

func TestValidate_ReportsAllIssuesAtOnce(t *testing.T) {
	m := baseMeta()
	m["apiVersion"] = 3
	m["key"] = "Bad Key"
	m["auth"] = map[string]any{"type": "query"}
	m["allowedHosts"] = []any{"127.0.0.1"}
	_, issues := Validate(mustJSON(t, m), without(fullHooks(), pluginproto.HookBuildQueryRequest))
	for _, p := range []string{"meta.apiVersion", "meta.key", "meta.auth.name", "meta.allowedHosts[0]", "meta.endpoints.video.mode"} {
		if !hasPath(issues, p) {
			t.Fatalf("期望一次报出 %q，实际：%+v", p, issues)
		}
	}
}

func TestValidate_ParsedMeta(t *testing.T) {
	got, issues := Validate(mustJSON(t, baseMeta()), fullHooks())
	if len(issues) != 0 {
		t.Fatalf("期望通过：%+v", issues)
	}
	if got.Key != "newapi" || got.Auth.Type != AuthBearer || got.Endpoints["video"].Mode != ModeAsync {
		t.Fatalf("解析结果不对：%+v", got)
	}
	if got.Poll == nil || got.Poll.Jitter != 0.2 || got.Import == nil || len(got.Import.Args) != 1 {
		t.Fatalf("poll / import 解析不对：%+v", got)
	}
	// channelSettings 保留书写顺序：用手写 JSON 验证（map 编码会排序）
	raw := `{"apiVersion":1,"key":"k","name":"n","version":"1.0.0","auth":{"type":"none"},
		"endpoints":{"text":{"mode":"sync"}},
		"channelSettings":{"zeta":{"type":"string","label":"Z"},"alpha":{"type":"number","label":"A","default":3}}}`
	got, issues = Validate([]byte(raw), fullHooks())
	if len(issues) != 0 {
		t.Fatalf("期望通过：%+v", issues)
	}
	if len(got.ChannelSettings) != 2 || got.ChannelSettings[0].Name != "zeta" || got.ChannelSettings[1].Name != "alpha" {
		t.Fatalf("设置项顺序不对：%+v", got.ChannelSettings)
	}
}
