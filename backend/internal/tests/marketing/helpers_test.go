package marketing_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/stepkit"
)

// newRegistry 注册全部营销环节，供各环节测试取用。
func newRegistry(t *testing.T) *stepkit.Registry {
	t.Helper()
	reg := stepkit.NewRegistry()
	marketing.RegisterSteps(reg)
	return reg
}

// getStep 按 key 取已注册的环节。
func getStep(t *testing.T, key string) stepkit.Step {
	t.Helper()
	st, ok := newRegistry(t).Get(key)
	if !ok {
		t.Fatalf("环节 %s 未注册", key)
	}
	return st
}

// mustJSON 把值编码成 JSON；字符串与 RawMessage 原样返回。
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	switch x := v.(type) {
	case json.RawMessage:
		return x
	case string:
		return json.RawMessage(x)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// runValidate 模拟协议层：先做 Schema 检查（有问题当作硬错误返回），再调环节的 Validate。
func runValidate(t *testing.T, step stepkit.Step, out any, in stepkit.Input) (stepkit.Flags, []string) {
	t.Helper()
	raw := mustJSON(t, out)
	if errs := stepkit.CheckSchema(step.Schema, raw); len(errs) > 0 {
		return nil, errs
	}
	flags, err := step.Validate(raw, in)
	return flags, stepkit.ErrorList(err)
}

// wantHard 断言硬错误里至少有一条含 sub。
func wantHard(t *testing.T, hard []string, sub string) {
	t.Helper()
	for _, h := range hard {
		if strings.Contains(h, sub) {
			return
		}
	}
	t.Fatalf("期望硬错误含 %q，实际 %v", sub, hard)
}

// wantNoHard 断言没有硬错误。
func wantNoHard(t *testing.T, hard []string) {
	t.Helper()
	if len(hard) > 0 {
		t.Fatalf("不应有硬错误：%v", hard)
	}
}

// goodFacts 是一份合格的事实卡。
func goodFacts() marketing.ProductFacts {
	return marketing.ProductFacts{
		Name:     "晨雾香水 50ml",
		Category: "香水",
		Anchors:  []string{"方形磨砂玻璃瓶身", "拉丝金色圆柱瓶颈与滚花瓶盖", "瓶身正面黑色 BRUME 字样"},
		SellingPoints: []marketing.SellingPoint{
			{Text: "木质调，留香约 8 小时", Proof: "清晨到傍晚的时间跳切", Source: "description"},
			{Text: "磨砂玻璃瓶身质感", Proof: "瓶身特写，光线下的磨砂纹理", Source: "image"},
		},
		Prohibited: []string{"不编造价格、销量、评价、认证与功效"},
		Audience:   "通勤女性",
		CTA:        "点击下方链接，把清晨带在身上",
	}
}

// goodReverse 是一份覆盖 10 秒、四个镜头的合格拆解。
func goodReverse() marketing.Reverse {
	mk := func(id int, start, end float64, opening bool, role string) marketing.ReverseShot {
		return marketing.ReverseShot{
			ID: id, Start: start, End: end, IsOpening3s: opening,
			ShotSize: "中近景", Camera: "固定", Subject: "年轻女性",
			Action: "女性单手握住软管，把乳霜挤到另一只手背上", Scene: "明亮的客厅窗边", Lighting: "柔和自然光",
			Person: "二十多岁女性，黑色马尾，米白毛衣", ProductInFrame: "软管占画面左侧约三分之一", ProductRole: role,
			KeyColors: []string{"橙色软管"},
		}
	}
	return marketing.Reverse{
		Overall: marketing.ReverseOverall{
			Style: "明亮生活化", NarrativeArc: "挤出—涂抹—展示", WhyViral: "动作直观，前三秒就看到产品用法",
			Transcript: "", Category: "护手霜",
		},
		Shots: []marketing.ReverseShot{
			mk(1, 0, 2.5, true, marketing.RoleDynamic),
			mk(2, 2.5, 5, true, marketing.RoleDynamic),
			mk(3, 5, 7.5, false, marketing.RoleNone),
			mk(4, 7.5, 10, false, marketing.RoleHeroReal),
		},
		Brands: []string{"HERALIFE"},
		Claims: []string{"全网销量第一"},
	}
}

// brief 返回测试用的轮次设置。
func brief(platform, lang string, duration int) marketing.Brief {
	return marketing.Brief{Platform: platform, Language: lang, Duration: duration, AspectRatio: "9:16", Description: "木质调，留香 8 小时左右，适合通勤"}
}

// goodShots 把 [0,duration] 平均切成 n 段，每段带旁白。
func goodShots(n int, duration float64, narration string) []marketing.ScriptShot {
	out := make([]marketing.ScriptShot, n)
	step := duration / float64(n)
	for i := range out {
		out[i] = marketing.ScriptShot{
			Start: float64(i) * step, End: float64(i+1) * step, Purpose: "proof",
			Shot: "中景", Camera: "固定", Scene: "女性在窗边喷香水", Narration: narration,
		}
	}
	out[n-1].End = duration
	return out
}
