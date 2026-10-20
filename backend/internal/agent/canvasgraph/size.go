package canvasgraph

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

// 节点画布尺寸的规则，与前端 web/src/utils/canvas/node-frame.ts 的 nodeFrameSize / resolveNodeAspect 一致，
// 两边读同一份测试向量 tests/testdata/canvasgraph/node-size-cases.json。
const (
	nodeShortSide = 324.0 // 图片、视频、音频节点框的短边
	textNodeW     = 576.0 // 文本和导演台节点：宽 576，比例 16:9
	textNodeH     = 324.0
)

// SizeHint 补充节点自己说不清的信息：模型默认的画幅。canvasgraph 不依赖模型配置，由调用方注入。
type SizeHint struct {
	// DefaultAspect 返回模型的比例参数默认值（如 "9:16"）；不知道或没有比例参数返回空串。
	// 只有节点完全没写比例参数时才会用到。
	DefaultAspect func(model string) string
}

// aspectPattern 是「宽:高」写法，与前端 parseAspect 一致，允许小数。
var aspectPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$`)

// parseAspect 把「16:9」读成宽/高；Auto、空值或写法不认识返回 0。
func parseAspect(v any) float64 {
	if v == nil {
		return 0
	}
	m := aspectPattern.FindStringSubmatch(fmt.Sprint(v))
	if m == nil {
		return 0
	}
	w, _ := strconv.ParseFloat(m[1], 64)
	h, _ := strconv.ParseFloat(m[2], 64)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w / h
}

// frameSize 按宽高比算节点框尺寸：短边固定 324，横图定高、竖图定宽，长边四舍五入。
func frameSize(aspect float64) (w, h float64) {
	if aspect <= 0 || math.IsNaN(aspect) || math.IsInf(aspect, 0) {
		aspect = 16.0 / 9
	}
	if aspect >= 1 {
		return math.Round(nodeShortSide * aspect), nodeShortSide
	}
	return nodeShortSide, math.Round(nodeShortSide / aspect)
}

// NodeSize 返回节点在画布上的尺寸。组取自己的 width/height（不小于最小尺寸）；
// 音频固定 1:1；文本和导演台固定 576×324；图片和视频按画幅算：
// 节点记下的 mediaAspect → params 里的 aspect_ratio（没有就看 ratio；写了但读不懂，如 Auto，直接按 16:9）
// → 参数完全没写时取模型默认比例 → 16:9。成片实测的画幅只有前端知道，由前端兜底。
func NodeSize(n Node, hint SizeHint) (w, h float64) {
	if n.IsGroup() {
		return math.Max(groupMinW, n.Width()), math.Max(groupMinH, n.Height())
	}
	switch n.Kind() {
	case KindAudio:
		return nodeShortSide, nodeShortSide
	case KindImage, KindVideo:
		return frameSize(imageAspect(n, hint))
	default:
		return textNodeW, textNodeH
	}
}

// imageAspect 读出图片、视频节点的宽高比，读不出来返回 0（按 16:9）。
func imageAspect(n Node, hint SizeHint) float64 {
	d := n.Data()
	if a := num(d["mediaAspect"]); a > 0 {
		return a
	}
	params, _ := d["params"].(map[string]any)
	for _, key := range []string{"aspect_ratio", "ratio"} {
		if v, ok := params[key]; ok {
			return parseAspect(v)
		}
	}
	if hint.DefaultAspect != nil {
		return parseAspect(hint.DefaultAspect(str(d["model"])))
	}
	return 0
}
