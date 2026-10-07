package canvasgraph

import "fmt"

// ImageRef 是一个可以「看」的图片节点：节点标题和它当前产物的素材 id。
type ImageRef struct {
	NodeID  string
	Label   string
	AssetID string // 素材 id（十进制字符串）
}

// ImageRefs 取一批节点当前的图片素材，顺序与 ids 一致。节点必须存在、是图片节点并且已经有产物，
// 否则返回 ErrInvalid 并写明是哪个节点、为什么，让模型改正。
func ImageRefs(g *Graph, ids []string) ([]ImageRef, error) {
	out := make([]ImageRef, 0, len(ids))
	for _, id := range ids {
		n := g.Node(id)
		switch {
		case n == nil:
			return nil, fmt.Errorf("%w：节点 %q 不存在", ErrInvalid, id)
		case n.IsGroup() || n.Kind() != KindImage:
			return nil, fmt.Errorf("%w：节点 %q（%s）不是图片节点，只能查看图片节点的内容", ErrInvalid, id, n.Label())
		}
		asset := str(n.Data()["assetId"])
		if asset == "" {
			return nil, fmt.Errorf("%w：节点 %q（%s）还没有图片内容", ErrInvalid, id, n.Label())
		}
		out = append(out, ImageRef{NodeID: id, Label: n.Label(), AssetID: asset})
	}
	return out, nil
}
