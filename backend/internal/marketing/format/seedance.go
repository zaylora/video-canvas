package format

import (
	"fmt"
	"strings"

	"video-canvas/internal/marketing"
)

// seedanceLayout 是 Seedance 的分区块写法（设计 §6.6，结构借鉴 YouMind 的分区块思路）：
//
//	[基本参数] 画幅，时长，有声
//	[参考素材] @图片… 商品；@图片k+1 模特；@视频1 参考片
//	[整体视觉] 平台写法要点；角度或跟拍
//	[商品]     外观锚点逐条
//	[分镜]     每镜一行，主体 + 动作放在开头
//	[音乐与配音] 旁白语言与音色；配乐
//	[硬性约束] 外观一致、商品不出画、画面不出字、平台禁令、关键颜色
//
// 区块标题永远是中文；镜头内容与旁白的语言跟脚本一致。
func seedanceLayout(in Input, o options) string {
	var blocks []string
	blocks = append(blocks, fmt.Sprintf("[基本参数] %s，%s 秒，有声", aspect(in), marketing.FmtSec(totalDuration(in))))
	blocks = append(blocks, "[参考素材] "+strings.Join(seedanceRefLines(in), "\n"))
	if parts := styleParts(in, o); len(parts) > 0 {
		blocks = append(blocks, "[整体视觉] "+strings.Join(parts, "；"))
	}
	blocks = append(blocks, seedanceProduct(in, o))
	blocks = append(blocks, "[分镜]\n"+strings.Join(shotLines(in, o), "\n"))
	blocks = append(blocks, "[音乐与配音] "+soundLine(in))
	blocks = append(blocks, "[硬性约束] "+strings.Join(hardClauses(in, o), "；"))
	return strings.Join(blocks, "\n")
}

// seedanceRefLines 写参考素材：商品图、模特图、参考视频各一行；一样都没有时写一句说明。
func seedanceRefLines(in Input) []string {
	var lines []string
	if refs := imageRefs(in.ProductImages); refs != "" {
		lines = append(lines, fmt.Sprintf("%s：%s，%s。", refs, productName(in), productRefNote))
	}
	if in.HasModel {
		lines = append(lines, fmt.Sprintf("%s：%s。", modelRef(in), modelRefNote))
	}
	if hasRef(in) {
		lines = append(lines, "@视频1："+videoRefNote+"。")
	}
	if len(lines) == 0 {
		lines = append(lines, "无参考素材，商品外观以下方外观锚点为准。")
	}
	return lines
}

// seedanceProduct 写 [商品] 区块：商品名加外观锚点逐条。
func seedanceProduct(in Input, o options) string {
	list := anchors(in, o)
	head := fmt.Sprintf("[商品] %s", productName(in))
	if cat := strings.TrimSpace(in.Facts.Category); cat != "" {
		head += "（" + cat + "）"
	}
	if len(list) == 0 {
		return head
	}
	lines := []string{head + "，外观锚点，每个镜头都要保持："}
	for _, a := range list {
		lines = append(lines, "- "+a)
	}
	return strings.Join(lines, "\n")
}
