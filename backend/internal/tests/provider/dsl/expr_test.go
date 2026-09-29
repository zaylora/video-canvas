// 本文件：expr.go 的单元测试：模板渲染、设计文档示例表达式、自定义函数、表达式限制与预处理。

package dsl_test

import (
	"reflect"
	"strings"
	"testing"
	. "video-canvas/internal/provider/dsl"
)

func TestRenderValue(t *testing.T) {
	rc := &RenderContext{
		Input: map[string]any{"prompt": "一只猫", "duration": 5.0, "tags": []any{"a", "b"}},
		Model: map[string]any{"params": map[string]any{"webappId": "123"}},
		Ctx:   map[string]any{"webhook_url": "https://x.test/hook"},
	}
	tests := []struct {
		name string
		tpl  any
		want any
	}{
		{"整体表达式保留字符串类型", "${ input.prompt }", "一只猫"},
		{"整体表达式保留数字类型", "${ input.duration * 16 + 1 }", 81.0},
		{"整体表达式保留数组类型", "${ input.tags }", []any{"a", "b"}},
		{"整体表达式保留对象类型", "${ {a: 1, b: input.prompt} }", map[string]any{"a": 1, "b": "一只猫"}},
		{"字符串插值", "/run/${ model.params.webappId }/x", "/run/123/x"},
		{"多段插值", "${ input.prompt }-${ input.duration }", "一只猫-5"},
		{"插值里 nil 变成空串", "a-${ input.missing }-b", "a--b"},
		{"没有表达式原样返回", "plain text", "plain text"},
		{"转义 $${ 输出字面量", "$${ not expr }", "${ not expr }"},
		{"表达式里带花括号和右花括号字符串", "${ '}' + toString({x: 1}.x) }", "}1"},
		{"整数不带小数点", "n=${ 3.0 }", "n=3"},
		{"嵌套 map 递归渲染", map[string]any{"a": "${ input.prompt }", "b": []any{"${ ctx.webhook_url }", 7.0, true}, "c": nil},
			map[string]any{"a": "一只猫", "b": []any{"https://x.test/hook", 7.0, true}, "c": nil}},
		{"非字符串原样", 42.0, 42.0},
		{"空白包裹不算整体表达式", " ${ input.duration } ", " 5 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderValue(tt.tpl, rc)
			if err != nil {
				t.Fatalf("渲染失败：%v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("期望 %#v，实际 %#v", tt.want, got)
			}
		})
	}
}

func TestRenderValue_不修改入参(t *testing.T) {
	tpl := map[string]any{"a": []any{"${ 1 + 1 }"}}
	if _, err := RenderValue(tpl, &RenderContext{}); err != nil {
		t.Fatal(err)
	}
	if tpl["a"].([]any)[0] != "${ 1 + 1 }" {
		t.Fatalf("模板被修改：%#v", tpl)
	}
}

func TestRenderValue_错误(t *testing.T) {
	tests := []struct {
		name, tpl string
	}{
		{"缺少结束花括号", "${ input.a"},
		{"空表达式", "${  }"},
		{"未知变量", "${ secret }"},
		{"语法错误", "${ 1 + }"},
		{"字符串没有结束引号", "${ 'abc }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := RenderValue(tt.tpl, &RenderContext{}); err == nil {
				t.Fatal("期望报错")
			}
		})
	}
}

func TestEvalExpr_设计文档示例表达式(t *testing.T) {
	rc := &RenderContext{
		Input:  map[string]any{"duration": 10.0},
		Status: 200,
		Resp: map[string]any{
			"taskId":     "t-1",
			"promptTips": `{"node_errors":{}}`,
			"results": []any{
				map[string]any{"url": "https://a/x.mp4", "outputType": "mp4", "nodeId": "9", "text": nil},
				map[string]any{"url": "https://a/x.png", "outputType": "png", "nodeId": "10"},
			},
			"usage": map[string]any{"consumeMoney": 0.5},
		},
	}
	rc.Outputs = []any{
		map[string]any{"url": "u1", "type": "mp4"},
		map[string]any{"url": "u2", "type": "png"},
		map[string]any{"url": "u3", "type": "webm"},
	}

	tests := []struct {
		name string
		src  string
		want any
	}{
		{"map 对象字面量（设计文档写法）", "map(resp.results ?? [], {url: .url, type: .outputType})",
			[]any{map[string]any{"url": "https://a/x.mp4", "type": "mp4"}, map[string]any{"url": "https://a/x.png", "type": "png"}}},
		{"map 缺少 results 用默认空数组", "map(resp.nothing ?? [], {url: .url})", []any{}},
		{"filter in", "filter(outputs, .type in ['mp4','webm','mov'])",
			[]any{map[string]any{"url": "u1", "type": "mp4"}, map[string]any{"url": "u3", "type": "webm"}}},
		{"时长换帧数", "input.duration * 16 + 1", 161.0},
		{"node_errors 为空", "len(parseJSON(resp.promptTips ?? '{}')?.node_errors ?? {}) == 0", true},
		{"success 复合条件", "status == 200 && resp.taskId != nil && len(parseJSON(resp.promptTips ?? '{}')?.node_errors ?? {}) == 0", true},
		{"平台把 promptTips 写成空串（旧写法也不崩）", "len(parseJSON('').node_errors ?? {}) == 0", true},
		{"promptTips 是字符串 null", "len(parseJSON('null').node_errors ?? {}) == 0", true},
		{"可选链", "resp.usage?.consumeMoney", 0.5},
		{"可选链缺失", "resp.nothing?.consumeMoney", nil},
		{"matches 函数调用形式", "matches(toString(resp.taskId), '^t-\\\\d+$')", true},
		{"matches 函数不匹配", "matches('abc', '^x')", false},
		{"matches 运算符形式仍可用", "'abc' matches '^a'", true},
		{"matches 在表达式中间", "status == 200 && matches('X', '(?i)x')", true},
		{"闭包普通写法", "map(outputs, {.url})", []any{"u1", "u2", "u3"}},
		{"闭包内嵌对象字面量", "map(outputs, {{k: .url}})", []any{map[string]any{"k": "u1"}, map[string]any{"k": "u2"}, map[string]any{"k": "u3"}}},
		{"字符串键的对象字面量", "map(outputs, {'k': .url})", []any{map[string]any{"k": "u1"}, map[string]any{"k": "u2"}, map[string]any{"k": "u3"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalExpr(tt.src, rc)
			if err != nil {
				t.Fatalf("求值失败：%v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("期望 %#v，实际 %#v", tt.want, got)
			}
		})
	}
}

func TestEvalExpr_resp为nil时不崩(t *testing.T) {
	got, err := EvalExpr("map(resp.results ?? [], {url: .url})", &RenderContext{})
	if err != nil {
		t.Fatalf("求值失败：%v", err)
	}
	if !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("实际 %#v", got)
	}
}

func TestEvalBool(t *testing.T) {
	rc := &RenderContext{Status: 429, Resp: map[string]any{"msg": "余额不足"}}
	tests := []struct {
		name    string
		src     string
		want    bool
		wantErr bool
	}{
		{"429 命中", "status == 429 || status >= 500", true, false},
		{"不命中", "status == 200", false, false},
		{"中文正则", "matches(toString(resp.msg), '(?i)balance|余额')", true, false},
		{"结果不是 bool", "status", false, true},
		{"编译错误", "status ==", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalBool(tt.src, rc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("期望 %v，实际 %v", tt.want, got)
			}
		})
	}
}

func TestCustomFunctions(t *testing.T) {
	rc := &RenderContext{Resp: map[string]any{
		"a": nil, "b": "", "c": "x", "n": 3.9, "s": "42", "f": "7.5", "obj": map[string]any{"k": 1},
		"json": `{"a":[1,2],"big":2041791506008539138}`,
	}}
	tests := []struct {
		name    string
		src     string
		want    any
		wantErr bool
	}{
		{"parseJSON 对象", "parseJSON(resp.json).a[1]", 2, false},
		{"parseJSON 保留大整数", "parseJSON(resp.json).big", 2041791506008539138, false},
		{"parseJSON nil 得到空对象", "len(parseJSON(resp.a))", 0, false},
		{"parseJSON 空串得到空对象", "len(parseJSON(''))", 0, false},
		{"parseJSON 空串后取字段", "parseJSON('').node_errors ?? 'none'", "none", false},
		{"parseJSON 非法", "parseJSON('{bad')", nil, true},
		{"parseJSON 已是对象", "parseJSON(resp.obj).k", 1, false},
		{"coalesce 跳过 nil 和空串", "coalesce(resp.a, resp.b, resp.c, 'z')", "x", false},
		{"coalesce 全空", "coalesce(resp.a, resp.b)", nil, false},
		{"toString nil", "toString(resp.a)", "", false},
		{"toString 数字", "toString(3.0)", "3", false},
		{"toString 大整数", "toString(2041791506008539138)", "2041791506008539138", false},
		{"toString 对象", "toString(resp.obj)", `{"k":1}`, false},
		{"toString bool", "toString(true)", "true", false},
		{"toInt 小数截断", "toInt(resp.n)", 3, false},
		{"toInt 整数字符串", "toInt(resp.s)", 42, false},
		{"toInt 小数字符串", "toInt(resp.f)", 7, false},
		{"toInt nil 为 0", "toInt(resp.a)", 0, false},
		{"toInt 非法字符串", "toInt('abc')", nil, true},
		{"matches 空值按空串", "matches(resp.a, '^$')", true, false},
		{"matches 非法正则", "matches('a', '(')", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalExpr(tt.src, rc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("期望 %#v(%T)，实际 %#v(%T)", tt.want, tt.want, got, got)
			}
		})
	}
}

func TestExpr_限制(t *testing.T) {
	t.Run("节点数超限", func(t *testing.T) {
		src := strings.Repeat("1 + ", MaxExprNodes+10) + "1"
		if _, err := EvalExpr(src, &RenderContext{}); err == nil {
			t.Fatal("期望超限报错")
		}
	})
	t.Run("源码过长", func(t *testing.T) {
		src := "'" + strings.Repeat("a", MaxExprLen) + "'"
		if _, err := EvalExpr(src, &RenderContext{}); err == nil {
			t.Fatal("期望过长报错")
		}
	})
	t.Run("上下文里没有 secret 变量", func(t *testing.T) {
		for _, name := range []string{"secret", "secrets", "auth", "env"} {
			if _, err := EvalExpr(name, &RenderContext{}); err == nil {
				t.Fatalf("变量 %s 不应存在", name)
			}
		}
	})
	t.Run("regex 过长", func(t *testing.T) {
		src := "matches('a', '" + strings.Repeat("a", MaxRegexLen+1) + "')"
		if _, err := EvalExpr(src, &RenderContext{}); err == nil {
			t.Fatal("期望报错")
		}
	})
}

func TestPreprocessExpr(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"无改写", "a + b", "a + b"},
		{"matches 函数形式", "matches(a, 'x')", "regexMatches(a, 'x')"},
		{"matches 运算符形式不动", "a matches 'x'", "a matches 'x'"},
		{"对象字面量补花括号", "map(xs, {a: .b})", "map(xs, { {a: .b} })"},
		{"普通闭包不动", "map(xs, {.b})", "map(xs, {.b})"},
		{"非谓词函数里的对象字面量不动", "foo({a: 1})", "foo({a: 1})"},
		{"含中文字符串位置正确", "map(xs, {a: '中文'}) ?? matches(y, '审')", "map(xs, { {a: '中文'} }) ?? regexMatches(y, '审')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PreprocessExpr(tt.src); got != tt.want {
				t.Fatalf("期望 %q，实际 %q", tt.want, got)
			}
		})
	}
}

func TestStringify(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, ""}, {"a", "a"}, {true, "true"}, {5.0, "5"}, {5.25, "5.25"}, {1e21, "1000000000000000000000"},
		{int64(7), "7"}, {uint64(9), "9"}, {[]any{1, "a"}, `[1,"a"]`}, {map[string]any{"a": 1}, `{"a":1}`},
	}
	for _, tt := range tests {
		if got := Stringify(tt.in); got != tt.want {
			t.Errorf("Stringify(%#v) = %q, 期望 %q", tt.in, got, tt.want)
		}
	}
}
