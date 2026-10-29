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
	// ModelOps 按模型 key 查它支持的生成方式，用来校验 mode；不传就只检查 mode 与节点种类是否相符
	ModelOps ModelOpsFunc
	// Assets 是 create_node 里 assetId 指向的素材（调用方先校验归属并解析好）；没有的 assetId 一律不可用
	Assets map[string]AssetRef
}

// AssetRef 是已经解析好的一份图片素材：建已完成的图片节点要的信息。
type AssetRef struct {
	ID       string
	URL      string // 稳定访问地址 /files/*
	FileName string
	Width    int
	Height   int
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
	g     *Graph
	opt   Options
	idMap map[string]string
	refit map[string]bool // 成员有变动、结束时要贴合的组
	// pending 是还没有位置的新节点和新组，按创建顺序排；所有 ops 执行完、组框定型后才统一放置（延后放置）
	pending []string
	// explicit 是 ops 里显式指定了位置的节点：位置原样保留，但放完后要检查是否压住了别的节点
	explicit map[string]bool
	issues   []Issue
	current  int
}

// Apply 在 g 的拷贝上依次执行 ops。有任何一项不合法就整体不生效，返回 *ValidationError，
// 其中列出全部问题，让模型一次改对。成功时返回新图、tempId 映射和改动差异，g 本身不变。
func Apply(g *Graph, ops []Op, opt Options) (*Result, error) {
	a := &applier{g: g.Clone(), opt: opt, idMap: map[string]string{}, refit: map[string]bool{}, explicit: map[string]bool{}}
	for i, op := range ops {
		a.current = i
		if err := a.run(op); err != nil {
			a.issues = append(a.issues, Issue{Index: i, Message: err.Error()})
		}
	}
	if len(a.issues) > 0 {
		return nil, &ValidationError{Issues: a.issues}
	}
	// 先贴合本批组框、放置新对象，再消解分批扩组压住已有邻居的碰撞；在 finish 前完成，推动也进入撤销和 patch。
	a.g.refitGroups(a.refit)
	a.g.placeDeferred(a.pending)
	moved := a.g.pushGroupOverlaps(a.g.grownGroups(g, a.refit), a.explicit)
	res := finish(g, a.g, a.idMap)
	res.Warnings = a.g.overlapWarnings(a.explicit, a.refit, moved)
	return res, nil
}

// unpend 把节点从待放置列表里去掉（它已经有了位置）。
func (a *applier) unpend(id string) {
	for i, p := range a.pending {
		if p == id {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			return
		}
	}
}

// settle 在要改动一个还没放置的组的成员关系之前，先把它定下来：有新成员的组按当前大小立刻放置；
// 还是空的就不用放了，组框之后由加进来的成员决定。
func (a *applier) settle(grp Node) {
	pending := false
	for _, p := range a.pending {
		pending = pending || p == grp.ID()
	}
	if !pending {
		return
	}
	if len(a.g.members(grp.ID())) > 0 {
		a.g.fitGroup(grp.ID())
		skip := make(map[string]bool, len(a.pending))
		for _, p := range a.pending {
			skip[p] = true
		}
		grp.SetPosition(a.g.placeFreeSkipping(skip))
	}
	a.unpend(grp.ID())
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
	if _, ok := params["op"]; ok {
		return errors.New("params.op 不能直接写入，生成方式请用 mode 字段设置")
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

// setContent 写节点的内容：文本节点没有提示词，prompt 字段写的就是正文（data.text，前端按 Markdown 渲染，
// 下游引用的也是它），有内容算 done、清空回到 idle，旧的失败信息一并去掉；其余节点写提示词。
func setContent(n Node, content string) {
	if n.Kind() != KindScript {
		setPrompt(n, content)
		return
	}
	d := n.Data()
	d["text"] = content
	if strings.TrimSpace(content) == "" {
		d["status"] = "idle"
	} else {
		d["status"] = "done"
	}
	delete(d, "error")
	delete(d, "errorTaskRef")
}

// createNode 新建一个普通节点，可以直接放进某个组。
func (a *applier) createNode(op Op) error {
	if op.Kind == KindDirector {
		return errors.New("导演台节点只能由用户在画布上手动创建")
	}
	if op.Character != nil && op.Kind != KindCharacter {
		return errors.New("character 只能写在角色节点（kind 为 character）上")
	}
	var character map[string]any
	if op.Kind == KindCharacter {
		var err error
		if character, err = newAgentCharacter(op.Character); err != nil {
			return err
		}
	}
	if !validKind(op.Kind) {
		return fmt.Errorf("节点种类 %q 不认识，可选 script / image / video / audio", op.Kind)
	}
	if err := checkParams(op.Params); err != nil {
		return err
	}
	ref, err := a.assetFor(op)
	if err != nil {
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
	defaultLabel := kindLabel[op.Kind]
	if character != nil {
		defaultLabel = characterLabel(character)
	}
	n := Node{"id": id, "type": "canvas", "data": map[string]any{"kind": op.Kind, "label": a.uniqueLabel(op.Label, defaultLabel)}}
	if character != nil {
		n.Data()["character"] = character
	}
	if ref != nil {
		// 已有素材：节点直接是已完成的上传型图片（角色卡则是它的主图），字段和前端从素材创建节点时一致
		for k, v := range uploadedImageData(*ref) {
			if k != "label" && k != "kind" {
				n.Data()[k] = v
			}
		}
	}
	if op.Prompt != nil {
		setContent(n, *op.Prompt)
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
	if op.Mode != nil {
		if err := a.setMode(n, *op.Mode); err != nil {
			return err
		}
	}
	switch {
	case op.Position != nil:
		n.SetPosition(*op.Position)
		a.explicit[id] = true
	case parent != nil:
		n.SetPosition(a.g.placeInGroup(parent.ID()))
	default:
		// 位置先留空，所有 ops 执行完、组框定型后再统一放置
		n.SetPosition(Point{})
		a.pending = append(a.pending, id)
	}
	if parent != nil {
		n["parentId"] = parent.ID()
		a.refit[parent.ID()] = true
	}
	a.g.Nodes = append(a.g.Nodes, n)
	return nil
}

// assetFor 取 create_node 的 assetId 对应的素材。没带 assetId 返回 nil；带了的只能是图片节点，
// 素材必须已由调用方解析好，且节点是已有素材，不再有提示词、模型、生成方式和参数。
func (a *applier) assetFor(op Op) (*AssetRef, error) {
	if op.AssetID == "" {
		return nil, nil
	}
	if generationKind(op.Kind) != KindImage {
		return nil, errors.New("assetId 只能用在图片节点和角色卡上")
	}
	if op.Prompt != nil || op.Model != nil || op.Mode != nil || len(op.Params) > 0 {
		return nil, errors.New("带 assetId 的节点是已有素材，不需要提示词、模型、生成方式和参数")
	}
	ref, ok := a.opt.Assets[op.AssetID]
	if !ok {
		return nil, fmt.Errorf("素材 %q 不可用：只能使用用户随消息发来的图片附件", op.AssetID)
	}
	return &ref, nil
}

// updateNode 修改普通节点的标题、提示词、模型和生成参数（params 浅合并），角色卡还能改人物数据。产物字段不在 Op 里，写不了。
func (a *applier) updateNode(op Op) error {
	if op.AssetID != "" {
		return errors.New("assetId 只能用在新建图片节点上")
	}
	n, err := a.resolve(op.ID)
	if err != nil {
		return err
	}
	if n.IsGroup() {
		return errors.New("组请用 set_group 修改")
	}
	if n.Kind() == KindDirector {
		return errors.New("导演台节点只能由用户在导演台里修改")
	}
	if op.Label == nil && op.Prompt == nil && op.Model == nil && len(op.Params) == 0 && op.Mode == nil && op.Character == nil {
		return errors.New("没有要修改的内容")
	}
	if op.Character != nil {
		if n.Kind() != KindCharacter {
			return errors.New("character 只能改角色节点（kind 为 character）")
		}
		if err := patchAgentCharacter(n, op.Character); err != nil {
			return err
		}
	}
	if err := checkParams(op.Params); err != nil {
		return err
	}
	d := n.Data()
	if op.Label != nil {
		d["label"] = a.uniqueLabel(op.Label, n.Label())
	}
	if op.Prompt != nil {
		setContent(n, *op.Prompt)
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
	if op.Mode != nil {
		return a.setMode(n, *op.Mode)
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
	a.explicit[n.ID()] = true
	a.unpend(n.ID())
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
	if src.Kind() == KindDirector || dst.Kind() == KindDirector {
		return errors.New("导演台不能连线：要先在导演台里导出成视频或图片节点")
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
