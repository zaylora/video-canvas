package format

import "video-canvas/internal/marketing"

// sampleInput 是后台展示样例稿用的内置输入：晨雾香水、抖音、15 秒、三个镜头。
func sampleInput() Input {
	return Input{
		Facts: marketing.ProductFacts{
			Name:     "BRUME 晨雾香水",
			Category: "香水",
			Anchors: []string{
				"方形磨砂玻璃瓶身，内装淡金色香水液",
				"拉丝金色圆柱瓶颈，顶部是滚花圆形金色瓶盖",
				"瓶身正面中央是黑色大写衬线字体「BRUME」",
			},
		},
		Script: marketing.Script{
			Angle: "pain", Title: "早八来不及", Hook: "出门前三十秒",
			Shots: []marketing.ScriptShot{
				{Start: 0, End: 5, Purpose: "hook", Shot: "近景", Camera: "手持推近", Scene: "女性在玄关抓起桌上的香水瓶，看了一眼手表", Narration: "早八又要迟到了？"},
				{Start: 5, End: 10, Purpose: "proof", Shot: "特写", Camera: "固定", Scene: "女性单手握住香水瓶，朝手腕喷了一下，雾气在光线里散开", Narration: "喷一下，木质调慢慢散开"},
				{Start: 10, End: 15, Purpose: "cta", Shot: "中景", Camera: "缓慢拉远", Scene: "女性拎起包走出门，香水瓶留在玄关柜上", Narration: "点下方链接，带一瓶晨雾上班"},
			},
			Music: "轻快的钢琴，中速", Voice: "温柔女声",
		},
		Platform:      marketing.PlatformOf("douyin"),
		Language:      marketing.LangZH,
		AspectRatio:   "9:16",
		Duration:      15,
		ProductImages: 1,
	}
}

// SampleOf 用内置示例输入渲染出样例稿，给后台展示格式化器的写法。纯函数；渲染失败时返回错误说明。
func SampleOf(f VideoFormatter) string {
	out, err := f.Render(sampleInput())
	if err != nil {
		return err.Error()
	}
	return out
}
