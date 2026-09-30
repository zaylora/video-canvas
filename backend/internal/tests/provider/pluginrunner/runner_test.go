package pluginrunner_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginproto"
	"video-canvas/internal/provider/pluginrunner"
)

func sha(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func newClient(opts pluginrunner.Options) plugin.RunnerClient {
	return plugin.NewInProcessRunnerClient(pluginrunner.NewServer(opts).Handler())
}

// callHook 装载插件并调用一个钩子，返回调用响应。
func callHook(t *testing.T, c plugin.RunnerClient, code, hook string, args ...string) *pluginproto.CallResponse {
	t.Helper()
	ctx := context.Background()
	loaded, err := c.Load(ctx, sha(code), code)
	if err != nil || !loaded.OK {
		t.Fatalf("装载失败：%v %+v", err, loaded)
	}
	raws := make([]json.RawMessage, 0, len(args))
	for _, a := range args {
		raws = append(raws, json.RawMessage(a))
	}
	resp, err := c.Call(ctx, sha(code), hook, raws, 0)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// 顶层死循环：预检在时限内返回问题，runner 仍然健康，之后还能正常装载别的插件。
func TestPrecheckTopLevelInfiniteLoopIsInterrupted(t *testing.T) {
	c := newClient(pluginrunner.Options{LoadTimeout: 200 * time.Millisecond})
	start := time.Now()
	resp, err := c.Precheck(context.Background(), `while (true) {}`)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("顶层死循环没有在时限内被中断：%v", elapsed)
	}
	if resp.OK || len(resp.Issues) == 0 || !strings.Contains(resp.Issues[0].Message, "超时") {
		t.Fatalf("预检应报告超时：%+v", resp)
	}
	if err := c.Ready(context.Background()); err != nil {
		t.Fatalf("runner 应仍然健康：%v", err)
	}
	ok := `module.exports = {meta: {}, buildSubmitRequest: function(){return 1}}`
	if loaded, err := c.Load(context.Background(), sha(ok), ok); err != nil || !loaded.OK {
		t.Fatalf("之后的装载应正常：%v %+v", err, loaded)
	}
}

func TestLoadTopLevelInfiniteLoopIsInterrupted(t *testing.T) {
	c := newClient(pluginrunner.Options{LoadTimeout: 200 * time.Millisecond})
	code := `module.exports = {}; for (;;) {}`
	resp, err := c.Load(context.Background(), sha(code), code)
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != pluginproto.CodeTimeout {
		t.Fatalf("装载死循环应返回 timeout：%+v", resp)
	}
	if err := c.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// meta 里的 toJSON 死循环同样要被中断。
func TestPrecheckMetaToJSONLoopIsInterrupted(t *testing.T) {
	c := newClient(pluginrunner.Options{LoadTimeout: 200 * time.Millisecond})
	code := `module.exports = {meta: {toJSON: function(){ while(true){} }}}`
	start := time.Now()
	resp, err := c.Precheck(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second || resp.OK || len(resp.Issues) == 0 {
		t.Fatalf("meta.toJSON 死循环应被中断并报问题：%+v", resp)
	}
}

// 钩子死循环被中断，之后同一插件还能再次调用。
func TestHookInfiniteLoopIsInterrupted(t *testing.T) {
	c := newClient(pluginrunner.Options{DefaultTimeout: 100 * time.Millisecond})
	code := `module.exports = {
  buildSubmitRequest: function(ctx) { if (ctx.loop) { while (true) {} } return {ok: true}; }
};`
	resp := callHook(t, c, code, "buildSubmitRequest", `{"loop":true}`)
	if resp.Error == nil || resp.Error.Code != pluginproto.CodeTimeout {
		t.Fatalf("死循环钩子应返回 timeout：%+v", resp)
	}
	resp, err := c.Call(context.Background(), sha(code), "buildSubmitRequest", []json.RawMessage{json.RawMessage(`{}`)}, 0)
	if err != nil || resp.Error != nil || !strings.Contains(string(resp.Result), "true") {
		t.Fatalf("超时之后应能继续调用：%v %+v", err, resp)
	}
}

// lockdown 必须在顶层代码之前生效：顶层拿不到 Function / eval，各种构造器路径也全部失效。
func TestLockdownBlocksDynamicCode(t *testing.T) {
	cases := []struct {
		name string
		body string // 钩子里执行的表达式，成功执行动态代码会返回 1
	}{
		{"AsyncFunction 构造器", `Object.getPrototypeOf(async function(){}).constructor("return 1")()`},
		{"GeneratorFunction 构造器", `Object.getPrototypeOf(function*(){}).constructor("return 1")()`},
		{"函数 constructor", `(function(){}).constructor("return 1")()`},
		{"箭头函数 constructor", `(() => 1).constructor("return 1")()`},
		{"全局 Function", `Function("return 1")()`},
		{"全局 eval", `eval("1")`},
		{"间接 eval", `(0, eval)("1")`},
		{"顶层捕获的 Function", `capturedFunction("return 1")()`},
		{"顶层捕获的 eval", `capturedEval("1")`},
		{"顶层捕获的 constructor", `capturedCtor("return 1")()`},
		{"globalThis.Function", `globalThis.Function("return 1")()`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(pluginrunner.Options{})
			code := `
var capturedFunction = typeof Function === "undefined" ? undefined : Function;
var capturedEval = typeof eval === "undefined" ? undefined : eval;
var capturedCtor;
try { capturedCtor = (function(){}).constructor; } catch (e) {}
module.exports = {
  buildSubmitRequest: function() { return ` + tc.body + `; }
};`
			resp := callHook(t, c, code, "buildSubmitRequest", `{}`)
			if resp.Error == nil {
				t.Fatalf("动态代码不应执行成功：result=%s", resp.Result)
			}
		})
	}
}

// 顶层直接执行动态代码：装载阶段就应失败，而不是悄悄成功。
func TestLockdownBlocksTopLevelDynamicCode(t *testing.T) {
	for name, code := range map[string]string{
		"顶层 Function":      `var x = Function("return 1")(); module.exports = {};`,
		"顶层 AsyncFunction": `var x = Object.getPrototypeOf(async function(){}).constructor("return 1"); module.exports = {};`,
		"顶层 eval":          `eval("1"); module.exports = {};`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newClient(pluginrunner.Options{})
			resp, err := c.Load(context.Background(), sha(code), code)
			if err != nil {
				t.Fatal(err)
			}
			if resp.OK {
				t.Fatal("顶层动态代码应被拒绝")
			}
		})
	}
}

// channelSettings 与 import.args 的键序按书写顺序保留（管理端按此渲染表单），不能变成字母序。
func TestPrecheckKeepsSettingKeyOrder(t *testing.T) {
	c := newClient(pluginrunner.Options{})
	code := `module.exports = {
  meta: {
    apiVersion: 1, key: "order", name: "顺序", version: "1.0.0",
    auth: {type: "none"},
    endpoints: {text: {mode: "sync"}},
    channelSettings: {
      zeta: {type: "string", label: "Z"},
      alpha: {type: "string", label: "A"},
      mid: {type: "string", label: "M"}
    },
    import: {args: {zulu: {type: "string", label: "Z"}, alpha2: {type: "string", label: "A"}}}
  },
  buildSubmitRequest: function() { return {}; },
  parseSubmitResponse: function() { return {}; }
};`
	resp, err := c.Precheck(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Meta == nil {
		t.Fatalf("预检没有返回 meta：%+v", resp)
	}
	raw := string(resp.Meta)
	assertOrder(t, raw, `"zeta"`, `"alpha"`, `"mid"`)
	assertOrder(t, raw, `"zulu"`, `"alpha2"`)
}

func assertOrder(t *testing.T, raw string, keys ...string) {
	t.Helper()
	last := -1
	for _, k := range keys {
		i := strings.Index(raw, k)
		if i < 0 || i < last {
			t.Fatalf("键 %s 的顺序不对（期望 %v）：%s", k, keys, raw)
		}
		last = i
	}
}

// 未装载的 sha 返回 unknown_plugin；钩子缺失返回 hook_missing。
func TestCallErrors(t *testing.T) {
	c := newClient(pluginrunner.Options{})
	resp, err := c.Call(context.Background(), sha("nope"), "buildSubmitRequest", []json.RawMessage{json.RawMessage(`{}`)}, 0)
	if err != nil || resp.Error == nil || resp.Error.Code != pluginproto.CodeUnknownPlugin {
		t.Fatalf("应返回 unknown_plugin：%v %+v", err, resp)
	}
	resp = callHook(t, c, `module.exports = {}`, "buildSubmitRequest", `{}`)
	if resp.Error == nil || resp.Error.Code != pluginproto.CodeHookMissing {
		t.Fatalf("应返回 hook_missing：%+v", resp)
	}
}

// 日志随成功调用返回（归还运行时之前复制，不会被并发请求清空）。
func TestCallReturnsLogs(t *testing.T) {
	c := newClient(pluginrunner.Options{})
	code := `module.exports = {buildSubmitRequest: function() { utils.log("hi"); return 1; }}`
	resp := callHook(t, c, code, "buildSubmitRequest", `{}`)
	if resp.Error != nil || len(resp.Logs) != 1 || resp.Logs[0] != "hi" {
		t.Fatalf("日志不符合预期：%+v", resp)
	}
}
