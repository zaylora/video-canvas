package canvasgraph

import "fmt"

// GenSource 是一个节点发起生成所需的全部输入，从画布里读出来：节点自己的模型、提示词、参数，
// 以及连进来的上游文字和素材。组装成任务输入、校验和计价由 service 层按模型能力完成。
type GenSource struct {
	NodeID    string         // 节点 id
	Kind      string         // script / image / video / audio
	Label     string         // 标题
	Model     string         // 节点上选的模型 key，没选为空
	Prompt    string         // 节点自己的提示词
	Params    map[string]any // 节点的生成参数（不含素材引用）
	Texts     []string       // 上游文本节点的正文，按连线顺序，空文本不收
	Assets    []GenAsset     // 上游已经有产物的素材，按连线顺序
	Pending   int            // 上游里还没有产物的素材节点数，只作提示
	Status    string         // 节点当前状态
	HasOutput bool           // 节点是否已经有产物：生成不会覆盖已有产物
}

// GenAsset 是上游节点的一份素材。
type GenAsset struct {
	Kind    string // image / video / audio
	AssetID string // 素材 id（十进制字符串）
}

// Generation 读出节点发起生成所需的输入。节点不存在、是组、或者是不能生成的种类时返回 ErrInvalid。
func Generation(g *Graph, nodeID string) (*GenSource, error) {
	n := g.Node(nodeID)
	if n == nil {
		return nil, fmt.Errorf("%w：节点 %q 不存在", ErrInvalid, nodeID)
	}
	if n.IsGroup() {
		return nil, fmt.Errorf("%w：%q 是组，不能生成", ErrInvalid, nodeID)
	}
	d := n.Data()
	src := &GenSource{NodeID: nodeID, Kind: n.Kind(), Label: n.Label(), Model: str(d["model"]), Status: str(d["status"]), HasOutput: hasMedia(d)}
	if !validKind(src.Kind) {
		return nil, fmt.Errorf("%w：节点 %q 的种类不能生成", ErrInvalid, nodeID)
	}
	params, _ := d["params"].(map[string]any)
	src.Params = make(map[string]any, len(params))
	for k, v := range params {
		src.Params[k] = v
	}
	src.Prompt = str(src.Params["prompt"])
	if src.Prompt == "" {
		src.Prompt = str(d["prompt"])
	}
	delete(src.Params, "prompt")
	for _, e := range g.Edges {
		// 来源线只记派生关系，不是参考素材
		if e.Target() == nodeID && !e.IsSource() {
			src.addUpstream(g.Node(e.Source()))
		}
	}
	return src, nil
}

// addUpstream 把一个上游节点收进来：文本节点收正文，素材节点收素材 id，还没产物的只计数。
func (s *GenSource) addUpstream(up Node) {
	if up == nil || up.IsGroup() {
		return
	}
	d := up.Data()
	if up.Kind() == KindScript {
		if t := str(d["text"]); t != "" {
			s.Texts = append(s.Texts, t)
		}
		return
	}
	if id := str(d["assetId"]); id != "" {
		s.Assets = append(s.Assets, GenAsset{Kind: up.Kind(), AssetID: id})
		return
	}
	s.Pending++
}

// BindTasks 把已创建的生成任务绑定到节点：写 taskId 并把状态置为 running，
// 此后节点由任务 store 与前端回填驱动。只写这两个字段（外加清掉旧的错误），产物字段从不由这里写。
// nodes 里不存在的节点会被忽略（用户可能在批准期间删了它）。
func BindTasks(g *Graph, tasks map[string]string) *Result {
	after := g.Clone()
	for id, taskID := range tasks {
		n := after.Node(id)
		if n == nil || n.IsGroup() {
			continue
		}
		d := n.Data()
		d["taskId"], d["status"] = taskID, "running"
		delete(d, "error")
	}
	return finish(g, after, nil)
}
