package canvasgraph

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// maxLabelRunes 是节点和组标题的最大字数，与前端 NODE_LABEL_MAX 一致。
const maxLabelRunes = 40

// Options 是 Apply 的可选项。
type Options struct {
	// NewID 生成新节点、组、连线的 id，prefix 是 "n_" / "g_" / "e_"；不传用随机值。测试里注入可预期的实现。
	NewID func(prefix string) string
}

// newID 生成带前缀的 id。
func (o Options) newID(prefix string) string {
	if o.NewID != nil {
		return o.NewID(prefix)
	}
	b := make([]byte, 5)
	_, _ = rand.Read(b) // crypto/rand 在受支持的平台上不会失败
	return prefix + hex.EncodeToString(b)
}

// applier 在一份拷贝上依次执行操作，收集问题。
type applier struct {
	g       *Graph
	opt     Options
	idMap   map[string]string
	refit   map[string]bool // 成员有变动、结束时要贴合的组
	issues  []Issue
	current int
}

// Apply 在 g 的拷贝上依次执行 ops。有任何一项不合法就整体不生效，返回 *ValidationError，
// 其中列出全部问题，让模型一次改对。成功时返回新图、tempId 映射和改动差异，g 本身不变。
func Apply(g *Graph, ops []Op, opt Options) (*Result, error) {
	a := &applier{g: g.Clone(), opt: opt, idMap: map[string]string{}, refit: map[string]bool{}}
	for i, op := range ops {
		a.current = i
		if err := a.run(op); err != nil {
			a.issues = append(a.issues, Issue{Index: i, Message: err.Error()})
		}
	}
	if len(a.issues) > 0 {
		return nil, &ValidationError{Issues: a.issues}
	}
	for gid := range a.refit {
		a.g.fitGroup(gid)
	}
	return finish(g, a.g, a.idMap), nil
}

// run 执行一项操作。
func (a *applier) run(op Op) error {
	switch op.Op {
	case OpCreateNode:
		return a.createNode(op)
	case OpUpdateNode:
		return a.updateNode(op)
	case OpCreateGroup:
		return a.createGroup(op)
	case OpSetGroup:
		return a.setGroup(op)
	case OpConnect:
		return a.connect(op)
	case OpMove:
		return a.move(op)
	default:
		return fmt.Errorf("操作 %q 不认识", op.Op)
	}
}

// resolve 把 id 或 tempId 解析成真实节点。
func (a *applier) resolve(ref string) (Node, error) {
	if id, ok := a.idMap[ref]; ok {
		ref = id
	}
	if n := a.g.Node(ref); n != nil {
		return n, nil
	}
	return nil, fmt.Errorf("节点 %q 不存在", ref)
}

// resolveGroup 把 id 或 tempId 解析成组。
func (a *applier) resolveGroup(ref string) (Node, error) {
	n, err := a.resolve(ref)
	if err != nil {
		return nil, err
	}
	if !n.IsGroup() {
		return nil, fmt.Errorf("%q 不是组", ref)
	}
	return n, nil
}

// register 记录 tempId 到真实 id 的映射，重复的 tempId 会让后面的引用有歧义，直接拒绝。
func (a *applier) register(temp, id string) error {
	if temp == "" {
		return nil
	}
	if _, dup := a.idMap[temp]; dup {
		return fmt.Errorf("tempId %q 重复", temp)
	}
	a.idMap[temp] = id
	return nil
}

// uniqueLabel 规整标题（去首尾空白、多空白收成一个、最长 40 字）；为空取 base，并避开画布里已有的同名。
func (a *applier) uniqueLabel(raw *string, base string) string {
	text := ""
	if raw != nil {
		text = strings.Join(strings.Fields(*raw), " ")
	}
	if r := []rune(text); len(r) > maxLabelRunes {
		text = string(r[:maxLabelRunes])
	}
	if text != "" {
		return text
	}
	used := map[string]bool{}
	for _, n := range a.g.Nodes {
		used[n.Label()] = true
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		if c := fmt.Sprintf("%s %d", base, i); !used[c] {
			return c
		}
	}
}

// checkParams 拒绝 params 里直接引用素材的字段：素材归属在这里无法校验，需要参考素材时应该连线。
func checkParams(params map[string]any) error {
	for _, k := range mediaParamKeys {
		if _, ok := params[k]; ok {
			return fmt.Errorf("params.%s 不能直接写入，需要参考素材请用 connect 连线", k)
		}
	}
	return nil
}

// setPrompt 同时写 data.prompt 和 params.prompt：前端读 params.prompt，旧节点只有 data.prompt。
func setPrompt(n Node, prompt string) {
	d := n.Data()
	d["prompt"] = prompt
	params, _ := d["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
		d["params"] = params
	}
	params["prompt"] = prompt
}

// createNode 新建一个普通节点，可以直接放进某个组。
func (a *applier) createNode(op Op) error {
	if !validKind(op.Kind) {
		return fmt.Errorf("节点种类 %q 不认识，可选 script / image / video / audio", op.Kind)
	}
	if err := checkParams(op.Params); err != nil {
		return err
	}
	var parent Node
	if op.ParentGroup != "" {
		var err error
		if parent, err = a.resolveGroup(op.ParentGroup); err != nil {
			return err
		}
	}
	id := a.opt.newID("n_")
	if err := a.register(op.TempID, id); err != nil {
		return err
	}
	n := Node{"id": id, "type": "canvas", "data": map[string]any{"kind": op.Kind, "label": a.uniqueLabel(op.Label, kindLabel[op.Kind])}}
	if op.Prompt != nil {
		setPrompt(n, *op.Prompt)
	}
	if op.Model != nil {
		n.Data()["model"] = *op.Model
	}
	if len(op.Params) > 0 {
		params, _ := n.Data()["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
			n.Data()["params"] = params
		}
		for k, v := range op.Params {
			params[k] = v
		}
	}
	switch {
	case op.Position != nil:
		n.SetPosition(*op.Position)
	case parent != nil:
		n.SetPosition(a.g.placeInGroup(parent.ID()))
	default:
		n.SetPosition(a.g.placeFree())
	}
	if parent != nil {
		n["parentId"] = parent.ID()
		a.refit[parent.ID()] = true
	}
	a.g.Nodes = append(a.g.Nodes, n)
	return nil
}

// updateNode 修改普通节点的标题、提示词、模型和生成参数（params 浅合并）。产物字段不在 Op 里，写不了。
func (a *applier) updateNode(op Op) error {
	n, err := a.resolve(op.ID)
	if err != nil {
		return err
	}
	if n.IsGroup() {
		return errors.New("组请用 set_group 修改")
	}
	if op.Label == nil && op.Prompt == nil && op.Model == nil && len(op.Params) == 0 {
		return errors.New("没有要修改的内容")
	}
	if err := checkParams(op.Params); err != nil {
		return err
	}
	d := n.Data()
	if op.Label != nil {
		d["label"] = a.uniqueLabel(op.Label, n.Label())
	}
	if op.Prompt != nil {
		setPrompt(n, *op.Prompt)
	}
	if op.Model != nil {
		d["model"] = *op.Model
	}
	if len(op.Params) > 0 {
		params, _ := d["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
			d["params"] = params
		}
		for k, v := range op.Params {
			params[k] = v
		}
	}
	return nil
}

// move 改位置：成员的坐标相对组左上角，其余是画布绝对坐标。成员移动后组框会贴合。
func (a *applier) move(op Op) error {
	n, err := a.resolve(op.ID)
	if err != nil {
		return err
	}
	if op.Position == nil {
		return errors.New("move 需要 position")
	}
	n.SetPosition(*op.Position)
	if pid := n.ParentID(); pid != "" {
		a.refit[pid] = true
	}
	return nil
}

// connect 连线。两端必须是普通节点、不能自连，种类要接得上（与前端 DOWNSTREAM_KINDS 一致）；已有同向连线时什么都不做。
func (a *applier) connect(op Op) error {
	src, err := a.resolve(op.Source)
	if err != nil {
		return err
	}
	dst, err := a.resolve(op.Target)
	if err != nil {
		return err
	}
	if src.IsGroup() || dst.IsGroup() {
		return errors.New("组不能连线")
	}
	if src.ID() == dst.ID() {
		return errors.New("节点不能连到自己")
	}
	if !canLink(src.Kind(), dst.Kind()) {
		return fmt.Errorf("%s 节点不能连到 %s 节点（%s 的下游只能是 %s）",
			src.Kind(), dst.Kind(), src.Kind(), strings.Join(downstream[src.Kind()], "、"))
	}
	if a.g.hasEdge(src.ID(), dst.ID()) {
		return nil
	}
	a.g.Edges = append(a.g.Edges, Edge{"id": a.opt.newID("e_"), "source": src.ID(), "target": dst.ID()})
	return nil
}
