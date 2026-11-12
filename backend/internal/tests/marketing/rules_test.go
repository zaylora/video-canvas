package marketing_test

import (
	"testing"

	"video-canvas/internal/marketing"
)

// shot 构造一个只含画面与旁白的测试镜头。
func shot(scene, narration string) marketing.ScriptShot {
	return marketing.ScriptShot{Scene: scene, Narration: narration}
}

// hasFlag 判断结果里是否有指定镜头、类型与文本的软问题。
func hasFlag(flags []marketing.Flag, shotIdx int, typ, text string) bool {
	for _, f := range flags {
		if f.Shot == shotIdx && f.Type == typ && f.Text == text {
			return true
		}
	}
	return false
}

func TestScanScript_极限词(t *testing.T) {
	facts := marketing.ProductFacts{Name: "晨雾香水"}
	shots := []marketing.ScriptShot{
		shot("清晨窗边，手腕喷香水", "全网第一的香水"),
		shot("特写瓶身", "这是顶级配方，绝对安全"),
		shot("近景", "今天是第一次用，第一步先喷在手腕"),
	}
	flags := marketing.ScanScript(shots, "douyin", facts, nil)
	for _, want := range []struct {
		shot int
		text string
	}{{0, "全网第一"}, {1, "顶级"}, {1, "绝对"}} {
		if !hasFlag(flags, want.shot, marketing.FlagForbidden, want.text) {
			t.Errorf("应在镜头 %d 报极限词 %q，实际 %+v", want.shot, want.text, flags)
		}
	}
	for _, f := range flags {
		if f.Shot == 2 {
			t.Errorf("「第一次」「第一步」不是极限词，不应报：%+v", f)
		}
	}
}

func TestScanScript_画面描述也扫(t *testing.T) {
	shots := []marketing.ScriptShot{shot("字幕写着「国家级认证」的瓶身特写", "")}
	flags := marketing.ScanScript(shots, "xhs", marketing.ProductFacts{}, nil)
	if !hasFlag(flags, 0, marketing.FlagForbidden, "国家级") {
		t.Fatalf("画面描述里的极限词也要扫：%+v", flags)
	}
}

func TestScanScript_同一处只报一次(t *testing.T) {
	shots := []marketing.ScriptShot{shot("顶级质感", "顶级的香气，顶级的瓶身，绝对顶级")}
	flags := marketing.ScanScript(shots, "tmall", marketing.ProductFacts{}, nil)
	count := 0
	for _, f := range flags {
		if f.Shot == 0 && f.Type == marketing.FlagForbidden && f.Text == "顶级" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("同一镜头同一类型同一文本只报一次，实际 %d 次：%+v", count, flags)
	}
}

func TestScanScript_非国内平台不扫中文极限词(t *testing.T) {
	shots := []marketing.ScriptShot{shot("特写", "全网第一，顶级品质")}
	for _, p := range []string{"generic", "tiktok", "amazon", "reels"} {
		if flags := marketing.ScanScript(shots, p, marketing.ProductFacts{}, nil); len(flags) != 0 {
			t.Errorf("平台 %s 不应扫中文极限词：%+v", p, flags)
		}
	}
}

func TestScanScript_亚马逊英文禁用说法(t *testing.T) {
	shots := []marketing.ScriptShot{
		shot("blender on a table", "Our best seller, only $29.99 today"),
		shot("close up", "Free shipping and a big discount"),
		shot("cup", "Cheaper than other brands"),
		shot("cup", "Easy to carry, easy to clean"),
	}
	flags := marketing.ScanScript(shots, "amazon", marketing.ProductFacts{}, nil)
	for _, want := range []struct {
		shot int
		text string
	}{{0, "best seller"}, {0, "$29.99"}, {1, "free shipping"}, {1, "discount"}, {2, "cheaper than"}} {
		if !hasFlag(flags, want.shot, marketing.FlagForbidden, want.text) {
			t.Errorf("镜头 %d 应报 %q，实际 %+v", want.shot, want.text, flags)
		}
	}
	for _, f := range flags {
		if f.Shot == 3 {
			t.Errorf("正常旁白不应报：%+v", f)
		}
	}
	// 同样的话在 tiktok 不算价格促销违规。
	if fl := marketing.ScanScript(shots[:2], "tiktok", marketing.ProductFacts{}, nil); len(fl) != 0 {
		t.Errorf("tiktok 不扫亚马逊价格促销用语：%+v", fl)
	}
}

func TestScanScript_英文医疗功效全平台(t *testing.T) {
	shots := []marketing.ScriptShot{shot("hand cream tube", "It can cure dry skin and is clinically proven")}
	for _, p := range []string{"tiktok", "reels", "amazon", "generic"} {
		flags := marketing.ScanScript(shots, p, marketing.ProductFacts{}, nil)
		if !hasFlag(flags, 0, marketing.FlagForbidden, "cure") || !hasFlag(flags, 0, marketing.FlagForbidden, "clinically proven") {
			t.Errorf("平台 %s 应报医疗功效用语：%+v", p, flags)
		}
	}
	ok := []marketing.ScriptShot{shot("tube", "Secure your day, the procure team loves it")}
	if flags := marketing.ScanScript(ok, "tiktok", marketing.ProductFacts{}, nil); len(flags) != 0 {
		t.Errorf("secure / procure 不是 cure，不应报：%+v", flags)
	}
}

func TestScanScript_品牌(t *testing.T) {
	rev := &marketing.Reverse{Brands: []string{"HERALIFE", "  "}}
	shots := []marketing.ScriptShot{
		shot("橙色软管上印着 heralife 字样", "挤一点在手背"),
		shot("近景", "用 HeraLife 的都说好"),
		shot("近景", "一抹就好"),
	}
	flags := marketing.ScanScript(shots, "generic", marketing.ProductFacts{}, rev)
	if !hasFlag(flags, 0, marketing.FlagBrand, "HERALIFE") || !hasFlag(flags, 1, marketing.FlagBrand, "HERALIFE") {
		t.Fatalf("旁白与画面里的第三方品牌（不分大小写）都要报：%+v", flags)
	}
	for _, f := range flags {
		if f.Shot == 2 {
			t.Errorf("镜头 2 没有品牌：%+v", f)
		}
	}
	if fl := marketing.ScanScript(shots, "generic", marketing.ProductFacts{}, nil); len(fl) != 0 {
		t.Errorf("rev 为 nil 不扫品牌与背书：%+v", fl)
	}
}

func TestScanScript_背书无依据(t *testing.T) {
	rev := &marketing.Reverse{Claims: []string{"全网销量第一", "皮肤科医生推荐", "留香持久八小时"}}
	facts := marketing.ProductFacts{
		Name:          "晨雾香水",
		SellingPoints: []marketing.SellingPoint{{Text: "木质调，留香持久八小时左右", Proof: "清晨到傍晚的时间跳切"}},
	}
	shots := []marketing.ScriptShot{
		shot("手腕喷香水", "销量第一的香水就是它"),   // 命中「全网销量第一」的片段「销量第一」，事实卡无依据
		shot("医生点头", "皮肤科医生推荐，放心用"),   // 无依据
		shot("傍晚办公室", "留香持久八小时，下班还在"), // 事实卡有依据，不报
		shot("瓶身", "喷一下，心情就好了"),       // 无背书
	}
	flags := marketing.ScanScript(shots, "generic", facts, rev)
	if !hasFlag(flags, 0, marketing.FlagClaim, "全网销量第一") {
		t.Errorf("镜头 0 应报背书无依据：%+v", flags)
	}
	if !hasFlag(flags, 1, marketing.FlagClaim, "皮肤科医生推荐") {
		t.Errorf("镜头 1 应报背书无依据：%+v", flags)
	}
	for _, f := range flags {
		if f.Shot >= 2 && f.Type == marketing.FlagClaim {
			t.Errorf("有依据或没有背书的镜头不应报：%+v", f)
		}
	}
}

func TestScanScript_背书只看旁白(t *testing.T) {
	rev := &marketing.Reverse{Claims: []string{"全网销量第一"}}
	flags := marketing.ScanScript([]marketing.ScriptShot{shot("全网销量第一的海报", "")}, "generic", marketing.ProductFacts{}, rev)
	if len(flags) != 0 {
		t.Fatalf("背书只扫旁白：%+v", flags)
	}
}

func TestScanNarrations(t *testing.T) {
	narr := []string{"顶级好物", "正常旁白", "绝对好用"}
	scenes := []string{"", "近景"} // 比旁白短，缺的按空串
	flags := marketing.ScanNarrations(narr, scenes, "douyin", marketing.ProductFacts{}, nil)
	if !hasFlag(flags, 0, marketing.FlagForbidden, "顶级") || !hasFlag(flags, 2, marketing.FlagForbidden, "绝对") {
		t.Fatalf("改旁白后重扫应按下标报：%+v", flags)
	}
	if len(flags) != 2 {
		t.Fatalf("只应有 2 条：%+v", flags)
	}
}
