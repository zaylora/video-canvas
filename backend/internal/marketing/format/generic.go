package format

import (
	"fmt"
	"strings"

	"video-canvas/internal/marketing"
)

// genericLayout 是通用段落写法（h3 暂用同一写法）：内容与 seedance 相同，写成连续段落，没有区块标题。
func genericLayout(in Input, o options) string {
	var paras []string
	paras = append(paras, fmt.Sprintf("画幅 %s，时长 %s 秒，有声。", aspect(in), marketing.FmtSec(totalDuration(in))))
	paras = append(paras, "参考素材："+strings.Join(genericRefParts(in), "；")+"。")
	if parts := styleParts(in, o); len(parts) > 0 {
		paras = append(paras, "整体视觉："+strings.Join(parts, "；")+"。")
	}
	paras = append(paras, genericProduct(in, o))
	paras = append(paras, "分镜：\n"+strings.Join(shotLines(in, o), "\n"))
	paras = append(paras, soundLine(in)+"。")
	paras = append(paras, "硬性约束："+strings.Join(hardClauses(in, o), "；")+"。")
	return strings.Join(paras, "\n")
}

// genericRefParts 写参考素材的各项，用分号连成一句。
func genericRefParts(in Input) []string {
	var parts []string
	if refs := imageRefs(in.ProductImages); refs != "" {
		parts = append(parts, fmt.Sprintf("%s 是商品「%s」，%s", refs, productName(in), productRefNote))
	}
	if in.HasModel {
		parts = append(parts, fmt.Sprintf("%s 是%s", modelRef(in), modelRefNote))
	}
	if hasRef(in) {
		parts = append(parts, "@视频1 "+videoRefNote)
	}
	if len(parts) == 0 {
		parts = append(parts, "无参考素材，商品外观以下方外观锚点为准")
	}
	return parts
}

// genericProduct 写商品外观一段：外观锚点用分号连起来。
func genericProduct(in Input, o options) string {
	list := anchors(in, o)
	if len(list) == 0 {
		return fmt.Sprintf("商品：%s。", productName(in))
	}
	return fmt.Sprintf("商品外观（%s，每个镜头都要保持）：%s。", productName(in), strings.Join(list, "；"))
}
