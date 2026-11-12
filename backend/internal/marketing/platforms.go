package marketing

import (
	"fmt"
	"strings"
	"unicode"
)

// Platform 是一个投放平台的预设（设计 §6.5，调研未复核，上线前由运营校对）。
type Platform struct {
	Key         string   // 平台 key，如 douyin
	Name        string   // 显示名
	AspectRatio string   // 默认画幅，如 9:16；小红书、淘宝天猫写 3:4，由服务按模型能力降级
	Language    string   // 默认旁白语言 zh / en
	Style       string   // 写法要点（一句话），给 /marketing/options 与提示词用
	Bans        []string // 该平台额外禁令，人能读的短句，写进提示词的硬性约束
	Domestic    bool     // 国内平台：启用广告法极限词扫描
}

// platforms 按契约 §1.1 的顺序列出 7 个平台。
var platforms = []Platform{
	{
		Key: "douyin", Name: "抖音", AspectRatio: "9:16", Language: LangZH, Domestic: true,
		Style: "前 3 秒强钩子，节奏快，口语化",
		Bans:  []string{"不使用广告法极限词（最、第一、顶级等）", "不出现站外导流、联系方式或二维码"},
	},
	{
		Key: "xhs", Name: "小红书", AspectRatio: "3:4", Language: LangZH, Domestic: true,
		Style: "种草口吻，生活方式场景，强调质感与真实体验",
		Bans:  []string{"不使用广告法极限词（最、第一、顶级等）", "不夸大功效，不写医疗效果"},
	},
	{
		Key: "tmall", Name: "淘宝天猫", AspectRatio: "3:4", Language: LangZH, Domestic: true,
		Style: "卖点直给，商品居中，信息清楚",
		Bans:  []string{"不使用广告法极限词（最、第一、顶级等）", "不承诺未经证实的功效"},
	},
	{
		Key: "tiktok", Name: "TikTok", AspectRatio: "9:16", Language: LangEN,
		Style: "原生 UGC 感，第一人称口吻，真实生活场景，口语化英文",
		Bans:  []string{"不写医疗功效用语（cure、heal、clinically proven 等）"},
	},
	{
		Key: "amazon", Name: "亚马逊", AspectRatio: "16:9", Language: LangEN,
		Style: "产品演示为主，功能与使用场景清楚，克制平实",
		Bans: []string{
			"不提价格、促销、折扣、免邮",
			"不点名或影射竞品，不使用排名用语（best seller、#1 等）",
			"不写医疗功效用语（cure、heal、clinically proven 等）",
		},
	},
	{
		Key: "reels", Name: "Instagram Reels", AspectRatio: "9:16", Language: LangEN,
		Style: "质感与氛围优先，用画面叙事，旁白少而短",
		Bans:  []string{"不写医疗功效用语（cure、heal、clinically proven 等）"},
	},
	{
		Key: "generic", Name: "通用", AspectRatio: "9:16", Language: LangZH,
		Style: "不针对特定平台，通用带货写法",
	},
}

// Platforms 返回全部平台预设（副本，调用方可随意修改）。
func Platforms() []Platform {
	out := make([]Platform, len(platforms))
	for i, p := range platforms {
		p.Bans = append([]string(nil), p.Bans...)
		out[i] = p
	}
	return out
}

// PlatformOf 按 key 取平台；不认识的 key 按 generic。
func PlatformOf(key string) Platform {
	for _, p := range Platforms() {
		if p.Key == key {
			return p
		}
	}
	return Platforms()[len(platforms)-1]
}

// AngleInfo 是一个创意角度（设计 §6.4）。角度是协议的输入枚举，不是技能。
type AngleInfo struct {
	Key  string
	Name string
	Desc string
}

// angles 是内置的六个创意角度；「自动」由模型按商品挑，不在此列。
var angles = []AngleInfo{
	{"pain", "痛点-解决", "先点出一个具体的生活烦恼，再让商品出场把它解决"},
	{"feature", "卖点拆解", "逐个卖点，用画面证明：一个镜头讲清一个卖点"},
	{"lifestyle", "生活方式种草", "把商品放进一天里的真实场景，让人想要那种生活"},
	{"compare", "前后对比", "用同一场景的使用前后对比说话，不点名竞品"},
	{"unbox", "开箱体验", "从拆封到第一次使用，保留第一反应与细节"},
	{"mood", "质感大片", "以光影、材质与氛围为主，旁白少，像广告大片"},
}

// Angles 返回六个创意角度，顺序固定。
func Angles() []AngleInfo { return append([]AngleInfo(nil), angles...) }

// AngleValid 判断是不是六个创意角度之一；跟拍的 follow 不算。
func AngleValid(key string) bool {
	for _, a := range angles {
		if a.Key == key {
			return true
		}
	}
	return false
}

// AngleName 返回角度的中文名；follow 返回「跟拍」，不认识的原样返回。
func AngleName(key string) string {
	if key == AngleFollow {
		return "跟拍"
	}
	for _, a := range angles {
		if a.Key == key {
			return a.Name
		}
	}
	return key
}

// 份数上限。
const (
	maxCreateScripts = 4 // 无参考时创意脚本最多份数
	maxAdaptScripts  = 3 // 有参考时改编（创意）脚本最多份数；另有 1 份跟拍由调用方另算
)

// ScriptPlan 计算本轮创意脚本要写几份。
//
//   - 无参考：份数 = 角度数（1–4），角度为空时取 autoCount（1–4）；
//   - 有参考：跟拍固定 1 份由调用方另算，这里只算改编份数 = 角度数（≤3）或 autoCount（0–3）。
//
// 角度必须合法且不重复；angles 非空时优先于 autoCount。不合法返回中文说明的 error。
func ScriptPlan(hasRef bool, angles []string, autoCount int) (int, error) {
	if err := checkAngles(angles); err != nil {
		return 0, err
	}
	lo, hi := 1, maxCreateScripts
	if hasRef {
		lo, hi = 0, maxAdaptScripts
	}
	if len(angles) > 0 {
		if len(angles) > hi {
			return 0, fmt.Errorf("创意脚本份数应为 %d–%d，当前选了 %d 个角度", lo, hi, len(angles))
		}
		return len(angles), nil
	}
	if autoCount < lo || autoCount > hi {
		return 0, fmt.Errorf("自动选角度时份数应为 %d–%d", lo, hi)
	}
	return autoCount, nil
}

// checkAngles 检查角度合法且不重复。
func checkAngles(list []string) error {
	seen := map[string]bool{}
	for _, a := range list {
		if !AngleValid(a) {
			return fmt.Errorf("不支持的创意角度：%s", a)
		}
		if seen[a] {
			return fmt.Errorf("创意角度重复：%s", a)
		}
		seen[a] = true
	}
	return nil
}

// 旁白语速：中文每秒 4.5 字，英文每秒 2.6 词（设计 §6.10.3，默认值待校准）。
const (
	zhCharsPerSec = 4.5
	enWordsPerSec = 2.6
)

// NarrationLimit 返回给定时长下旁白的字数（中文）或词数（英文）上限，取整。
func NarrationLimit(language string, duration int) int {
	if language == LangEN {
		return int(float64(duration) * enWordsPerSec)
	}
	return int(float64(duration) * zhCharsPerSec)
}

// NarrationLength 统计旁白长度：中文按字符（去掉空白和标点），英文按空格分词（只含标点的片段不算词）。
func NarrationLength(language, text string) int {
	if language == LangEN {
		n := 0
		for _, w := range strings.Fields(text) {
			if strings.ContainsFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
				n++
			}
		}
		return n
	}
	n := 0
	for _, r := range text {
		if isPunctOrSpace(r) {
			continue
		}
		n++
	}
	return n
}
