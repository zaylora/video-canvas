package drama_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"video-canvas/internal/drama"
)

func TestSourceFiveMillionBoundary(t *testing.T) {
	for _, n := range []int{5_000_000, 5_000_001} {
		_, err := drama.ParseSource("书.txt", []byte(strings.Repeat("字", n)))
		if (err != nil) != (n > 5_000_000) {
			t.Fatalf("%d: %v", n, err)
		}
	}
}

func TestPreflightKindsAndUnits(t *testing.T) {
	for _, tc := range []struct{ text, kind, unit string }{
		{"第1集 开始\n甲：走。\n第2集 结束\n乙：好。", "script", "episode"},
		{"1-1 日 内\n甲：走。\n1-2 夜 外\n乙：好。\n1-3 日 内\n甲：回来。", "script", "scene"},
		{"第1章 开始\n他来了。\n第2章 结束\n她走了。", "novel", "chapter"},
		{"一段没有明显结构的文字。", "unknown", "chunk"},
	} {
		p := drama.Preflight(tc.text, "")
		if p.KindGuess != tc.kind || p.UnitKind != tc.unit {
			t.Fatalf("%+v", p)
		}
		for i, u := range p.Units {
			if u.No != i+1 || u.Text == "" {
				t.Fatalf("%+v", u)
			}
		}
	}
	p := drama.Preflight("求推荐票\n第1章 长章\n"+strings.Repeat("段落。\n", 4000)+"第2章 尾声\n结束。", "novel")
	if strings.Contains(p.Text, "求推荐票") || len(p.Units) < 3 {
		t.Fatalf("去噪或拆长章失败: %d", len(p.Units))
	}
	for _, u := range p.Units {
		if u.Chars > 12000 {
			t.Fatalf("长章未拆: %d", u.Chars)
		}
	}
}

func TestSampleAndBatchKeepCompleteSource(t *testing.T) {
	p := drama.Preflight(strings.Repeat("原文段落。\n", 15000), "novel")
	sample := drama.Sample(p.Units, 40000)
	if utf8.RuneCountInString(sample) > 40000 || !strings.Contains(sample, "[1]") {
		t.Fatalf("抽样预算错误: %d", utf8.RuneCountInString(sample))
	}
	cursor := 0
	for cursor < len(p.Units) {
		batch, err := drama.NextBatch(p.Units, cursor, len(p.Units), 30000)
		if err != nil || len(batch) == 0 || len(batch) > 12 {
			t.Fatalf("batch=%d err=%v", len(batch), err)
		}
		for i, u := range batch {
			if u.No != cursor+i+1 || !strings.Contains(drama.UnitSource(batch), u.Text) {
				t.Fatal("批次丢失正文或不连续")
			}
		}
		cursor = batch[len(batch)-1].No
	}
}

func TestScriptEpisodesKeepScenes(t *testing.T) {
	marked := drama.Preflight("第1集 开始\n甲：走。\n第2集 结束\n乙：好。", "script")
	if eps := drama.ScriptEpisodes(marked.Units, 60); len(eps) != 2 || !strings.Contains(eps[1].ScriptMD, "乙：好。") {
		t.Fatalf("%+v", eps)
	}
	preface := drama.Preflight("序言不应成为额外的一集。\n第1集 开始\n甲：走。\n第2集 结束\n乙：好。", "script")
	if eps := drama.ScriptEpisodes(preface.Units, 60); len(eps) != 2 || !strings.Contains(eps[0].ScriptMD, "序言") || eps[0].Title != "第1集 开始" {
		t.Fatalf("序言应并入首集: %+v", eps)
	}
	unmarked := drama.Preflight("1-1 日 内\n"+strings.Repeat("甲：走。\n", 180)+"1-2 夜 外\n乙：好。\n1-3 日 内\n甲：回来。", "script")
	eps := drama.ScriptEpisodes(unmarked.Units, 60)
	if len(eps) < 1 || strings.Count(eps[0].ScriptMD, "甲：走。") != 180 {
		t.Fatal("场次被拆开或台词丢失")
	}
}

func TestPromptBodyCannotReplaceProtocol(t *testing.T) {
	prompt, err := drama.PromptWithBody("scripts", map[string]any{"source": "原文"}, "只输出 Markdown")
	if err != nil || !strings.Contains(prompt, "只输出 Markdown") || !strings.Contains(prompt, "## 输出 JSON Schema") || !strings.Contains(prompt, "## 本步职责") {
		t.Fatalf("%s %v", prompt, err)
	}
	block := drama.SkillBlock("连续性", strings.Repeat("字", 20000))
	if !strings.Contains(block, "【写作方法：连续性】") || strings.Count(block, "字") != 16000 {
		t.Fatal("技能注入未按字截断")
	}
}
