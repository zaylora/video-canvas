package marketing_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/stepkit"
)

func TestRegisterSteps_四个环节(t *testing.T) {
	reg := stepkit.NewRegistry()
	marketing.RegisterSteps(reg)
	want := []struct {
		key, title string
		media      stepkit.MediaNeed
	}{
		{marketing.StepFacts, "商品事实卡", stepkit.MediaImages},
		{marketing.StepReverse, "拆解参考片", stepkit.MediaVideo},
		{marketing.StepFollow, "跟拍改写", stepkit.MediaNone},
		{marketing.StepCreate, "创意脚本", stepkit.MediaNone},
	}
	list := reg.List()
	if len(list) != len(want) {
		t.Fatalf("应注册 %d 个环节，实际 %d", len(want), len(list))
	}
	for i, w := range want {
		st := list[i]
		if st.Key != w.key || st.Title != w.title || st.Media != w.media || st.Studio != marketing.Studio {
			t.Errorf("第 %d 个环节不符：%s %s %s %s", i, st.Key, st.Title, st.Media, st.Studio)
		}
		if st.Duty == "" || len(st.Inputs) == 0 || st.Validate == nil || len(st.Samples) == 0 || st.Preview == nil {
			t.Errorf("环节 %s 字段不全", st.Key)
		}
		if len(st.Rules.Hard) == 0 {
			t.Errorf("环节 %s 缺硬错误文案", st.Key)
		}
		if n := len([]rune(st.Builtin)); n < 1200 || n > st.EffectiveMethodMax() {
			t.Errorf("环节 %s 内置方法 %d 字，应在 1200 到上限之间", st.Key, n)
		}
	}
}

func TestRegisterSteps_重复注册会失败(t *testing.T) {
	reg := stepkit.NewRegistry()
	marketing.RegisterSteps(reg)
	defer func() {
		if recover() == nil {
			t.Fatal("重复 MustRegister 应 panic")
		}
	}()
	marketing.RegisterSteps(reg)
}

func TestBuiltin_不规定输出格式(t *testing.T) {
	for _, st := range newRegistry(t).List() {
		for _, bad := range []string{"```json", "输出格式如下", "JSON Schema"} {
			if strings.Contains(st.Builtin, bad) {
				t.Errorf("环节 %s 的方法正文不应规定输出格式（含 %q）", st.Key, bad)
			}
		}
	}
}

func TestSchema_都能生成字段说明(t *testing.T) {
	for _, st := range newRegistry(t).List() {
		lines, err := stepkit.SchemaFields(st.Schema)
		if err != nil || len(lines) < 5 {
			t.Errorf("环节 %s 的 Schema 字段说明异常：%v %v", st.Key, lines, err)
		}
	}
}

func TestSamples_键与带参考标记(t *testing.T) {
	want := map[string]map[string]bool{
		marketing.StepFacts:   {"perfume": false, "blender": false},
		marketing.StepReverse: {"cream": true},
		marketing.StepFollow:  {"cream": true},
		marketing.StepCreate:  {"perfume": false, "blender": false, "cream": true},
	}
	for _, st := range newRegistry(t).List() {
		got := map[string]bool{}
		for _, sm := range st.Samples {
			got[sm.Key] = sm.NeedsRef
			if sm.Name == "" {
				t.Errorf("%s/%s 缺样例名", st.Key, sm.Key)
			}
		}
		if len(got) != len(want[st.Key]) {
			t.Errorf("环节 %s 样例 %v，期望 %v", st.Key, got, want[st.Key])
			continue
		}
		for k, ref := range want[st.Key] {
			if r, ok := got[k]; !ok || r != ref {
				t.Errorf("环节 %s 样例 %s 的 NeedsRef 应为 %v", st.Key, k, ref)
			}
		}
	}
}

func TestSamples_能构造输入并拼出提示词(t *testing.T) {
	for _, st := range newRegistry(t).List() {
		for _, sm := range st.Samples {
			t.Run(st.Key+"/"+sm.Key, func(t *testing.T) {
				in, err := sm.Build(context.Background())
				if err != nil {
					t.Fatalf("构造输入失败：%v", err)
				}
				prompt, err := stepkit.BuildPrompt(st, stepkit.BuiltinMethod(st), in, nil)
				if err != nil || len(prompt) < 500 {
					t.Fatalf("拼提示词失败或过短：%v（%d）", err, len(prompt))
				}
				switch st.Media {
				case stepkit.MediaImages:
					if len(in.Images) == 0 || !strings.HasPrefix(in.Images[0], "data:image/jpeg;base64,") {
						t.Errorf("看图环节样例应带 jpeg data URL 图片")
					}
				case stepkit.MediaNone:
					if len(in.Images) != 0 || in.Video != nil {
						t.Errorf("纯文本环节样例不应带媒体")
					}
				}
			})
		}
	}
}

func TestSamples_拆解样例带视频和兜底截帧(t *testing.T) {
	st := getStep(t, marketing.StepReverse)
	sm, _ := st.Sample("cream")
	in, err := sm.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if in.Video == nil || in.Video.MimeType != "video/mp4" {
		t.Fatalf("应内联 mp4：%+v", in.Video)
	}
	raw, err := base64.StdEncoding.DecodeString(in.Video.Data)
	if err != nil || len(raw) < 100_000 {
		t.Fatalf("视频 Base64 不对：%v %d", err, len(raw))
	}
	if len(in.Images) != 8 {
		t.Fatalf("应带 8 帧兜底截帧，实际 %d", len(in.Images))
	}
	for _, f := range in.Images {
		if !strings.HasPrefix(f, "data:image/jpeg;base64,") {
			t.Fatalf("截帧应是 jpeg data URL")
		}
	}
	if in.Vars["duration"] != 10.0 {
		t.Fatalf("样例时长应为 10 秒：%v", in.Vars["duration"])
	}
}

// 样例自带的拆解与事实卡必须过各自的 Schema 与 Validate，保证试跑下游环节时输入是合法的。
func TestSamples_自带的拆解和事实卡合法(t *testing.T) {
	reg := newRegistry(t)
	reverseStep, _ := reg.Get(marketing.StepReverse)
	factsStep, _ := reg.Get(marketing.StepFacts)
	check := func(t *testing.T, in stepkit.Input) {
		t.Helper()
		if v, ok := in.Vars["facts"]; ok {
			_, hard := runValidate(t, factsStep, v, factsInput("样例描述"))
			wantNoHard(t, hard)
		}
		if v, ok := in.Vars["reverse"]; ok {
			_, hard := runValidate(t, reverseStep, v, reverseInput(10))
			wantNoHard(t, hard)
			rev, err := marketing.DecodeReverse(mustJSON(t, v))
			if err != nil || len(rev.Brands) == 0 || rev.Brands[0] != "HERALIFE" {
				t.Fatalf("样例拆解应带 brands=HERALIFE：%+v %v", rev.Brands, err)
			}
		}
	}
	for _, key := range []string{marketing.StepFollow, marketing.StepCreate} {
		st, _ := reg.Get(key)
		for _, sm := range st.Samples {
			t.Run(key+"/"+sm.Key, func(t *testing.T) {
				in, err := sm.Build(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := in.Vars["facts"]; !ok {
					t.Fatal("下游环节样例必须自带事实卡")
				}
				if sm.NeedsRef {
					if _, ok := in.Vars["reverse"]; !ok {
						t.Fatal("带参考的样例必须自带拆解")
					}
				}
				check(t, in)
			})
		}
	}
}

func TestSamples_跟拍样例的商品事实卡品牌中立(t *testing.T) {
	st := getStep(t, marketing.StepFollow)
	sm, _ := st.Sample("cream")
	in, _ := sm.Build(context.Background())
	facts, err := marketing.DecodeFacts(mustJSON(t, in.Vars["facts"]))
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(append(append([]string{facts.Name}, facts.Anchors...), facts.Prohibited...), " ")
	if strings.Contains(strings.ToUpper(all), "HERALIFE") {
		t.Fatalf("我方事实卡不应带第三方品牌：%s", all)
	}
}

func TestPreview_人能读(t *testing.T) {
	reg := newRegistry(t)
	cases := map[string]any{
		marketing.StepFacts:   goodFacts(),
		marketing.StepReverse: goodReverse(),
		marketing.StepFollow:  goodFollow(),
		marketing.StepCreate:  marketing.CreateOutput{Scripts: []marketing.Script{script("pain", "好")}},
	}
	for key, out := range cases {
		st, _ := reg.Get(key)
		p := st.Preview(mustJSON(t, out))
		if p == "" || strings.HasPrefix(strings.TrimSpace(p), "{") {
			t.Errorf("环节 %s 的摘要应是人能读的文字：%q", key, p)
		}
	}
	st, _ := reg.Get(marketing.StepFacts)
	if p := st.Preview([]byte("not json")); p == "" {
		t.Error("解码失败也要有兜底文字")
	}
}
