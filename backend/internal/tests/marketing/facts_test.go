package marketing_test

import (
	"strings"
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/stepkit"
)

func factsInput(desc string) stepkit.Input {
	b := brief("douyin", "zh", 15)
	b.Description = desc
	return marketing.FactsInput(b, []string{"data:image/jpeg;base64,AAAA"})
}

func TestFactsInput_变量与图片(t *testing.T) {
	in := marketing.FactsInput(brief("tiktok", "en", 15), []string{"data:image/jpeg;base64,AAAA", "https://x/y.jpg"})
	if len(in.Images) != 2 {
		t.Fatalf("商品图应放进 Images：%v", in.Images)
	}
	plat, ok := in.Vars["platform"].(map[string]any)
	if !ok || plat["key"] != "tiktok" || plat["name"] == "" || plat["style"] == "" {
		t.Fatalf("Vars[platform] 应含 key/name/style/bans：%v", in.Vars["platform"])
	}
	if _, ok := plat["bans"]; !ok {
		t.Fatalf("Vars[platform] 缺 bans")
	}
	if in.Vars["language"] != "en" || in.Vars["description"] != "木质调，留香 8 小时左右，适合通勤" {
		t.Fatalf("语言与描述不对：%v", in.Vars)
	}
}

func TestFactsValidate_合格通过(t *testing.T) {
	st := getStep(t, marketing.StepFacts)
	flags, hard := runValidate(t, st, goodFacts(), factsInput("木质调"))
	wantNoHard(t, hard)
	if len(flags) != 0 {
		t.Fatalf("合格事实卡不应有软问题：%+v", flags)
	}
}

func TestFactsValidate_硬错误(t *testing.T) {
	st := getStep(t, marketing.StepFacts)
	tests := []struct {
		name   string
		mutate func(f *marketing.ProductFacts)
		desc   string
		want   string
	}{
		{"没有外观锚点", func(f *marketing.ProductFacts) { f.Anchors = nil }, "木质调", "anchors"},
		{"锚点超过 8 条", func(f *marketing.ProductFacts) { f.Anchors = make([]string, 9); fill(f.Anchors) }, "木质调", "anchors"},
		{"卖点超过 5 条", func(f *marketing.ProductFacts) {
			f.SellingPoints = make([]marketing.SellingPoint, 6)
			for i := range f.SellingPoints {
				f.SellingPoints[i] = marketing.SellingPoint{Text: "卖点", Proof: "画面", Source: "image"}
			}
		}, "木质调", "selling_points"},
		{"卖点没有了", func(f *marketing.ProductFacts) { f.SellingPoints = nil }, "木质调", "selling_points"},
		{"来源不合法", func(f *marketing.ProductFacts) { f.SellingPoints[0].Source = "guess" }, "木质调", "source"},
		{"名称为空", func(f *marketing.ProductFacts) { f.Name = "" }, "木质调", "name"},
		{"用户没写描述却出现 source=description", func(f *marketing.ProductFacts) {}, "", "description"},
		{"描述只有空白", func(f *marketing.ProductFacts) {}, "   ", "description"},
		{"卖点文字过长", func(f *marketing.ProductFacts) { f.SellingPoints[0].Text = strings.Repeat("好", 200) }, "木质调", "text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := goodFacts()
			tt.mutate(&f)
			_, hard := runValidate(t, st, f, factsInput(tt.desc))
			wantHard(t, hard, tt.want)
		})
	}
}

func TestFactsValidate_缺字段(t *testing.T) {
	st := getStep(t, marketing.StepFacts)
	_, hard := runValidate(t, st, `{"name":"x","category":"y","anchors":["a"],"selling_points":[{"text":"t","proof":"p","source":"image"}]}`, factsInput("d"))
	wantHard(t, hard, "audience")
}

func TestFactsValidate_无描述时只有看得见的卖点可以通过(t *testing.T) {
	st := getStep(t, marketing.StepFacts)
	f := goodFacts()
	f.SellingPoints[0].Source = "image"
	_, hard := runValidate(t, st, f, factsInput(""))
	wantNoHard(t, hard)
}

func TestValidateFacts(t *testing.T) {
	if err := marketing.ValidateFacts(goodFacts()); err != nil {
		t.Fatalf("合格事实卡应通过：%v", err)
	}
	f := goodFacts()
	f.Name = ""
	f.Anchors = nil
	f.SellingPoints[1].Source = "x"
	f.CTA = ""
	list := stepkit.ErrorList(marketing.ValidateFacts(f))
	if len(list) < 4 {
		t.Fatalf("应逐条返回问题，实际 %v", list)
	}
	f = goodFacts()
	f.Anchors = make([]string, 9)
	fill(f.Anchors)
	wantHard(t, stepkit.ErrorList(marketing.ValidateFacts(f)), "anchors")
}

func TestDecodeFacts(t *testing.T) {
	got, err := marketing.DecodeFacts(mustJSON(t, goodFacts()))
	if err != nil || got.Name != "晨雾香水 50ml" || len(got.SellingPoints) != 2 {
		t.Fatalf("解码结果不对：%+v err=%v", got, err)
	}
	if _, err := marketing.DecodeFacts([]byte(`{"name":`)); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
}

// fill 把切片填成非空字符串。
func fill(s []string) {
	for i := range s {
		s[i] = "锚点"
	}
}
