package canvasgraph

import (
	"errors"
	"fmt"
)

// checkHue 校验颜色名；空串表示清除。
func checkHue(c *string) error {
	if c != nil && *c != "" && !hues[*c] {
		return fmt.Errorf("颜色 %q 不认识，可选 red / orange / yellow / green / cyan / blue / purple / pink", *c)
	}
	return nil
}

// setHue 写入或清除组的颜色字段。
func setHue(d map[string]any, key string, c *string) {
	if c == nil {
		return
	}
	if *c == "" {
		delete(d, key)
		return
	}
	d[key] = *c
}

// memberNodes 解析成员引用，要求都是普通节点。
func (a *applier) memberNodes(refs []string) ([]Node, error) {
	out := make([]Node, 0, len(refs))
	for _, ref := range refs {
		n, err := a.resolve(ref)
		if err != nil {
			return nil, err
		}
		if n.IsGroup() {
			return nil, fmt.Errorf("组 %q 不能放进另一个组", ref)
		}
		out = append(out, n)
	}
	return out, nil
}

// createGroup 新建组。给了 memberIds 就用成员的包围盒加留白做组框，成员视觉位置不变；
// 没有成员时放在现有内容右侧，组框取最小尺寸。
func (a *applier) createGroup(op Op) error {
	if err := checkHue(op.Color); err != nil {
		return err
	}
	if err := checkHue(op.LabelColor); err != nil {
		return err
	}
	members, err := a.memberNodes(op.MemberIDs)
	if err != nil {
		return err
	}
	id := a.opt.newID("g_")
	if err := a.register(op.TempID, id); err != nil {
		return err
	}
	grp := Node{"id": id, "type": "group", "data": map[string]any{"label": a.uniqueLabel(op.Label, "组")}}
	setHue(grp.Data(), "color", op.Color)
	setHue(grp.Data(), "labelColor", op.LabelColor)
	pos, w, h := a.g.placeFree(), groupMinW, groupMinH
	if op.Position != nil {
		pos = *op.Position
	}
	if len(members) > 0 {
		rs := make([]rect, len(members))
		for i, m := range members {
			rs[i] = a.g.absRect(m)
		}
		pos, w, h = frameAround(rs)
	}
	grp.SetPosition(pos)
	grp["width"], grp["height"] = w, h
	a.g.Nodes = append(a.g.Nodes, grp)
	for _, m := range members {
		a.g.reparent(m, grp)
	}
	return nil
}

// setGroup 改组名和颜色，增删成员。加入和移出都保持节点的视觉位置不变；成员有变动时组框随后贴合。
func (a *applier) setGroup(op Op) error {
	grp, err := a.resolveGroup(op.ID)
	if err != nil {
		return err
	}
	if err := checkHue(op.Color); err != nil {
		return err
	}
	if err := checkHue(op.LabelColor); err != nil {
		return err
	}
	if op.Label == nil && op.Color == nil && op.LabelColor == nil && len(op.AddMembers) == 0 && len(op.RemoveMembers) == 0 {
		return errors.New("没有要修改的内容")
	}
	add, err := a.memberNodes(op.AddMembers)
	if err != nil {
		return err
	}
	remove, err := a.memberNodes(op.RemoveMembers)
	if err != nil {
		return err
	}
	for _, m := range remove {
		if m.ParentID() != grp.ID() {
			return fmt.Errorf("节点 %q 不在组 %q 里", m.ID(), grp.ID())
		}
	}
	if op.Label != nil {
		grp.Data()["label"] = a.uniqueLabel(op.Label, grp.Label())
	}
	setHue(grp.Data(), "color", op.Color)
	setHue(grp.Data(), "labelColor", op.LabelColor)
	for _, m := range add {
		a.g.reparent(m, grp)
	}
	for _, m := range remove {
		a.g.reparent(m, nil)
	}
	if len(add)+len(remove) > 0 {
		a.refit[grp.ID()] = true
	}
	return nil
}

// Delete 删除节点和连线。删节点会一并删除它的连线；删组时成员保留，回到画布绝对坐标。
// 这是破坏性操作，调用方必须在用户批准之后才调用。
func Delete(g *Graph, nodeIDs, edgeIDs []string) (*Result, error) {
	if len(nodeIDs)+len(edgeIDs) == 0 {
		return nil, fmt.Errorf("%w: 没有指定要删除的内容", ErrInvalid)
	}
	work := g.Clone()
	gone, err := existingSet(nodeIDs, "节点", func(id string) bool { return work.Node(id) != nil })
	if err != nil {
		return nil, err
	}
	drop, err := existingSet(edgeIDs, "连线", func(id string) bool { return work.edgeIndex(id) >= 0 })
	if err != nil {
		return nil, err
	}
	// 先让被删组的成员脱离，再删节点，这样成员的绝对坐标是按组删除前算的。
	for _, n := range work.Nodes {
		if !n.IsGroup() || !gone[n.ID()] {
			continue
		}
		for _, m := range work.members(n.ID()) {
			if !gone[m.ID()] {
				work.reparent(m, nil)
			}
		}
	}
	work.Nodes = filter(work.Nodes, func(n Node) bool { return !gone[n.ID()] })
	work.Edges = filter(work.Edges, func(e Edge) bool {
		return !drop[e.ID()] && !gone[e.Source()] && !gone[e.Target()]
	})
	return finish(g, work, nil), nil
}

// existingSet 把 id 列表转成集合，并要求每个 id 都满足 exists；kind 只用来写错误说明。
func existingSet(ids []string, kind string, exists func(string) bool) (map[string]bool, error) {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !exists(id) {
			return nil, fmt.Errorf("%w: %s %q 不存在", ErrInvalid, kind, id)
		}
		set[id] = true
	}
	return set, nil
}

// filter 返回满足条件的元素，不修改入参。
func filter[T any](s []T, keep func(T) bool) []T {
	out := make([]T, 0, len(s))
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}
