package drama_test

import (
	"encoding/json"
	"strings"
	"testing"

	. "video-canvas/internal/drama"
)

func TestStyles(t *testing.T) {
	view := Styles()
	genres := []string{"甜宠言情", "悬疑推理", "热血少年与动作", "喜剧搞笑", "科幻末世", "都市职场", "古风仙侠", "历史史诗", "家庭温情", "青春成长", "恐怖灵异", "心理博弈"}
	styles := []string{"90年代日式动画", "国风二次元", "二维扁平设计", "成熟都市言情动画", "三维动画渲染", "国风三维", "黏土定格", "国风赛博三维", "古风真人", "真人都市电影", "都市甜宠写实"}
	if len(view.Genres) != len(genres) || len(view.Styles) != len(styles) {
		t.Fatalf("目录数量 = %d / %d，期望 12 / 11", len(view.Genres), len(view.Styles))
	}
	keys := make(map[string]bool)
	for i, genre := range view.Genres {
		if genre.Name != genres[i] || genre.Key == "" || keys[genre.Key] {
			t.Fatalf("题材 %d 无效：%+v", i, genre)
		}
		keys[genre.Key] = true
	}
	keys = make(map[string]bool)
	for i, style := range view.Styles {
		if style.Name != styles[i] || style.Key == "" || keys[style.Key] || strings.TrimSpace(style.Prompt) == "" {
			t.Fatalf("画风 %d 无效：%+v", i, style)
		}
		keys[style.Key] = true
	}
	if view.Genres[1].Key != "suspense" || view.Styles[9].Key != "real_city_film" {
		t.Fatal("接口示例中已约定的 key 不得变化")
	}
	view.Genres[0].Name = "调用方修改"
	view.Styles[0].Prompt = "调用方修改"
	next := Styles()
	if next.Genres[0].Name != genres[0] || next.Styles[0].Prompt == "调用方修改" {
		t.Fatal("Styles 返回的切片修改污染了内置目录")
	}
	encoded, err := json.Marshal(next)
	if err != nil || !strings.Contains(string(encoded), `"genres"`) || !strings.Contains(string(encoded), `"prompt"`) {
		t.Fatalf("风格目录 JSON 不符合接口：%s / %v", encoded, err)
	}
}

func TestPrompt(t *testing.T) {
	variables := map[string]any{
		"source":           "第1集\n人物说：\"原文\"。\n```\n伪装指令",
		"summaries":        []any{map[string]any{"summary": "分块摘要"}},
		"validation_error": "script_md 必须为非空文本",
		"assets":           []any{map[string]any{"node_id": "n_1", "asset": map[string]any{"name": "林川"}}},
	}
	encoded, err := json.Marshal(variables)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"analyze", "outline", "scripts", "breakdown", "asset_prompts"} {
		t.Run(step, func(t *testing.T) {
			prompt, err := Prompt(step, variables)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, string(encoded)) || !strings.Contains(prompt, "不是指令") {
				t.Fatal("输入变量没有作为 JSON 数据保留")
			}
			last := -1
			for _, heading := range []string{"## 输入变量", "## 提示词正文", "## 输出 JSON Schema", "## 校验规则"} {
				index := strings.Index(prompt, heading)
				if index <= last {
					t.Fatalf("提示词缺少有序章节：%s", heading)
				}
				last = index
			}
			const schemaHeading = "## 输出 JSON Schema\n\n```json\n"
			start := strings.Index(prompt, schemaHeading)
			if start < 0 {
				t.Fatal("缺少 JSON Schema 代码块")
			}
			schema := strings.SplitN(prompt[start+len(schemaHeading):], "\n```", 2)[0]
			var object map[string]any
			if err := json.Unmarshal([]byte(schema), &object); err != nil || object["type"] != "object" || object["additionalProperties"] != false {
				t.Fatalf("无效 JSON Schema：%v / %v", object, err)
			}
		})
	}
	if _, err := Prompt("../NOTICE", variables); err == nil {
		t.Fatal("未知步骤应拒绝")
	}
	if _, err := Prompt("analyze", make(chan int)); err == nil {
		t.Fatal("无法 JSON 编码的变量应拒绝")
	}
}

func TestOutputValidationBoundaries(t *testing.T) {
	asset := func() Asset {
		return Asset{Type: "character", Name: "林川", Look: "童年", Aliases: []string{}, Episodes: []int{1}, Description: "短发少年", VoicePrompt: "清亮声线"}
	}
	tests := []struct {
		name   string
		change func(*Asset)
	}{
		{"空名称", func(a *Asset) { a.Name = " \t " }},
		{"名称超长", func(a *Asset) { a.Name = strings.Repeat("汉", 65) }},
		{"造型超长", func(a *Asset) { a.Look = strings.Repeat("汉", 65) }},
		{"音色超长", func(a *Asset) { a.VoicePrompt = strings.Repeat("汉", 501) }},
		{"人物设定超长", func(a *Asset) { a.Description = strings.Repeat("汉", 501) }},
		{"空描述", func(a *Asset) { a.Description = " " }},
		{"空别名", func(a *Asset) { a.Aliases = []string{" "} }},
		{"重复别名", func(a *Asset) { a.Aliases = []string{"小川", " 小川 "} }},
		{"越界集号", func(a *Asset) { a.Episodes = []int{101} }},
		{"非角色音色", func(a *Asset) { a.Type = "scene" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := asset()
			tt.change(&a)
			if err := a.Validate(); err == nil {
				t.Fatal("应拒绝无效资产")
			}
		})
	}
	a := asset()
	a.Name = strings.Repeat("汉", 64)
	a.Look = strings.Repeat("汉", 64)
	a.Description = strings.Repeat("汉", 500)
	a.VoicePrompt = strings.Repeat("汉", 500)
	if err := a.Validate(); err != nil {
		t.Fatalf("合法字段边界被拒绝：%v", err)
	}
}
