package canvasgraph

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// 生成方式（画布上视频、图片节点生成面板顶部的页签）。节点上选中的方式存在 data.params.op；
// 取值必须是模型 capabilities.ops 里支持的，与 modelcfg 里的 Op* 常量一致（本包不依赖 provider，所以这里重写一份）。
var (
	videoOps = []string{"t2v", "i2v", "omni"} // 文生视频 / 图生视频 / 全能参考
	imageOps = []string{"t2i", "i2i"}         // 文生图 / 图生图
)

// ModelOpsFunc 按模型 key 返回它支持的生成方式；known 为 false 表示不认识这个模型（没上架或已下架）。
type ModelOpsFunc func(model string) (ops []string, known bool)

// ModeInfo 是节点当前的生成方式，随 canvas_get_state 的详情给模型看。
type ModeInfo struct {
	// Op 是节点上已保存且模型仍支持的生成方式；空表示没选：生成时有参考图且模型支持图生就走图生，否则取模型支持的第一种
	Op string `json:"op"`
	// Options 是这个模型可选的生成方式；Auto 为真时为空
	Options []string `json:"options,omitempty"`
	// Auto 为真表示模型按有没有参考图自动判断（文生图 + 图生图），不需要也不能设置
	Auto bool `json:"auto,omitempty"`
}

// isAutoOps 判断模型是不是自动判断的图片模型：同时支持文生图和图生图。
func isAutoOps(ops []string) bool { return slices.Contains(ops, "t2i") && slices.Contains(ops, "i2i") }

// PickOp 选生成方式，生成和提示都按这一条规则：同时支持文生图和图生图的模型由有没有参考图决定；
// 其它取节点上选的（仍被支持时），没选时有参考图且模型支持图生视频就走图生，最后取模型支持的第一种。
// 模型没有生成方式（文本、音频）返回空。
func PickOp(ops []string, chosen any, hasImage bool) string {
	switch {
	case len(ops) == 0:
		return ""
	case isAutoOps(ops):
		if hasImage {
			return "i2i"
		}
		return "t2i"
	}
	if c, ok := chosen.(string); ok && slices.Contains(ops, c) {
		return c
	}
	if hasImage && slices.Contains(ops, "i2v") {
		return "i2v"
	}
	return ops[0]
}

// setMode 把 mode 写进节点的 params.op。方式要符合节点种类；模型已知时还要是它支持的方式。
func (a *applier) setMode(n Node, mode string) error {
	var allowed []string
	switch n.Kind() {
	case KindVideo:
		allowed = videoOps
	case KindImage:
		allowed = imageOps
	default:
		return errors.New("只有图片、视频节点有生成方式（mode）")
	}
	if !slices.Contains(allowed, mode) {
		if !slices.Contains(videoOps, mode) && !slices.Contains(imageOps, mode) {
			return fmt.Errorf("生成方式 %q 不认识：视频节点可选 %s，图片节点可选 %s",
				mode, strings.Join(videoOps, " / "), strings.Join(imageOps, " / "))
		}
		return fmt.Errorf("%s节点的生成方式只能是 %s，不能是 %s", kindLabel[n.Kind()], strings.Join(allowed, " / "), mode)
	}
	model := str(n.Data()["model"])
	if model == "" {
		return errors.New("先用 model 给节点选好模型再设置 mode：可选的生成方式由模型决定")
	}
	if a.opt.ModelOps != nil {
		if ops, known := a.opt.ModelOps(model); known {
			if isAutoOps(ops) {
				return fmt.Errorf("模型 %s 按有没有参考图自动判断生成方式（文生图 / 图生图），不用设置 mode", model)
			}
			if !slices.Contains(ops, mode) {
				return fmt.Errorf("模型 %s 不支持 %s，可选：%s", model, mode, strings.Join(ops, " / "))
			}
		}
	}
	d := n.Data()
	params, _ := d["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
		d["params"] = params
	}
	params["op"] = mode
	return nil
}

// AnnotateModes 给详情里图片、视频节点补上生成方式：节点选了已知模型才有。
func AnnotateModes(res *DetailResult, fn ModelOpsFunc) {
	if res == nil || fn == nil {
		return
	}
	for i := range res.Nodes {
		n := &res.Nodes[i]
		if n.Type == "group" {
			continue
		}
		if kind := str(n.Data["kind"]); kind != KindVideo && kind != KindImage {
			continue
		}
		model := str(n.Data["model"])
		if model == "" {
			continue
		}
		ops, known := fn(model)
		if !known {
			continue
		}
		info := &ModeInfo{Auto: isAutoOps(ops)}
		if !info.Auto {
			info.Options = slices.Clone(ops)
			params, _ := n.Data["params"].(map[string]any)
			if saved := str(params["op"]); slices.Contains(ops, saved) {
				info.Op = saved
			}
		}
		n.Mode = info
	}
}

// ModeWarnings 检查这些节点的生成方式和上游连线是否对得上，返回给模型看的提醒（不拦截）：
// 文生视频连了参考素材（参考会被忽略）、图生视频连了视频或音频（只引用图片）。
// 没连参考素材不警告：上游可能稍后才连、或还没出图，提交生成时前端和后端都放行这种情况。
func ModeWarnings(g *Graph, ids []string, fn ModelOpsFunc) []string {
	if fn == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		n := g.Node(id)
		if n == nil || seen[id] || n.IsGroup() || n.Kind() != KindVideo {
			continue
		}
		seen[id] = true
		ops, known := fn(str(n.Data()["model"]))
		if !known {
			continue
		}
		up := g.upstreamKinds(id)
		if up[KindImage]+up[KindVideo]+up[KindAudio] == 0 {
			continue
		}
		params, _ := n.Data()["params"].(map[string]any)
		switch PickOp(ops, params["op"], up[KindImage] > 0) {
		case "t2v":
			out = append(out, fmt.Sprintf("节点 %q（%s）当前是文生视频，已连的参考素材不会被用到。%s", id, n.Label(), t2vAdvice(ops)))
		case "i2v":
			if up[KindVideo]+up[KindAudio] > 0 {
				msg := fmt.Sprintf("节点 %q（%s）当前是图生视频，只引用图片，连进来的视频、音频不会被用到。", id, n.Label())
				if slices.Contains(ops, "omni") {
					msg += "要一起引用请用 mode 改成 omni（全能参考）。"
				}
				out = append(out, msg)
			}
		}
	}
	return out
}

// t2vAdvice 是文生视频连了参考素材时的建议：只推荐该模型确实支持的方式。
func t2vAdvice(ops []string) string {
	var opts []string
	if slices.Contains(ops, "i2v") {
		opts = append(opts, "i2v（图生视频，只引用图片）")
	}
	if slices.Contains(ops, "omni") {
		opts = append(opts, "omni（全能参考，可同时引用图片、视频、音频）")
	}
	if len(opts) == 0 {
		return "该模型只支持文生视频，参考素材用不上，可以断开连线或换一个模型。"
	}
	return "需要参考素材请用 mode 改成 " + strings.Join(opts, " 或 ") + "。"
}

// upstreamKinds 统计连进某节点的上游各是什么种类。
func (g *Graph) upstreamKinds(id string) map[string]int {
	out := map[string]int{}
	for _, e := range g.Edges {
		if e.Target() != id {
			continue
		}
		if src := g.Node(e.Source()); src != nil && !src.IsGroup() {
			out[src.Kind()]++
		}
	}
	return out
}
