package canvasgraph

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// GenSource 是一个节点发起生成所需的全部输入，从画布里读出来：节点自己的模型、提示词、参数，
// 以及连进来的上游文字和素材。组装成任务输入、校验和计价由 service 层按模型能力完成。
type GenSource struct {
	NodeID string         // 节点 id
	Kind   string         // script / image / video / audio
	Label  string         // 标题
	Model  string         // 节点上选的模型 key，没选为空
	Prompt string         // 节点自己的提示词
	Params map[string]any // 节点的生成参数（不含素材引用）
	Texts  []GenText      // 上游文本节点的正文，按连线顺序，空文本不收
	Assets []GenAsset     // 上游已经有产物的素材：普通连线按连线顺序在前，角色卡展开的素材在后
	// Characters 是连进来的角色卡：节点 id → 角色名，提示词里 @ 角色卡时展开成角色名
	Characters map[string]string
	Pending    int    // 上游里还没有产物的素材节点数，只作提示
	Status     string // 节点当前状态
	HasOutput  bool   // 节点是否已经有产物：生成不会覆盖已有产物
}

// GenText 是上游文本节点的正文。
type GenText struct {
	NodeID string // 文本节点 id：提示词里 @ 它时按它认出是哪一段
	Text   string // 正文
}

// GenAsset 是上游节点的一份素材。
type GenAsset struct {
	NodeID  string // 素材节点 id：提示词里 @ 它时按它认出是哪一份
	Kind    string // image / video / audio
	AssetID string // 素材 id（十进制字符串）
	// FromCharacter 表示这份素材是角色卡展开的（主图、补充图或声音）：排在普通连线之后，放不下时先被截掉
	FromCharacter bool
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
	src := &GenSource{NodeID: nodeID, Kind: generationKind(n.Kind()), Label: n.Label(), Model: str(d["model"]), Status: str(d["status"]), HasOutput: hasMedia(d)}
	if !validKind(src.Kind) || src.Kind == KindDirector {
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
		if e.Target() == nodeID {
			src.addUpstream(g.Node(e.Source()))
		}
	}
	// 普通连线先占参考名额，角色卡展开的素材排在后面（与前端 resolveBindings 一致）；稳定排序保持各自的连线顺序
	slices.SortStableFunc(src.Assets, func(a, b GenAsset) int {
		switch {
		case a.FromCharacter == b.FromCharacter:
			return 0
		case a.FromCharacter:
			return 1
		default:
			return -1
		}
	})
	return src, nil
}

// addUpstream 把一个上游节点收进来：文本节点收正文，素材节点收素材 id，还没产物的只计数。
// 导演台连出来的线只表示导出物的出处，不是参考输入，跳过。
func (s *GenSource) addUpstream(up Node) {
	if up == nil || up.IsGroup() || up.Kind() == KindDirector {
		return
	}
	d := up.Data()
	if up.Kind() == KindCharacter {
		s.addCharacter(up)
		return
	}
	if up.Kind() == KindScript {
		if t := str(d["text"]); t != "" {
			s.Texts = append(s.Texts, GenText{NodeID: up.ID(), Text: t})
		}
		return
	}
	if id := str(d["assetId"]); id != "" {
		s.Assets = append(s.Assets, GenAsset{NodeID: up.ID(), Kind: generationKind(up.Kind()), AssetID: id})
		return
	}
	s.Pending++
}

// RefIDs 按种类挑出要交给模型的参考素材 id（规则与前端 resolveBindings 一致）：
//   - 普通连线的素材全部带上（同一份去重），超出上限交给任务校验报错；
//   - 角色卡展开的素材只填普通素材用剩的名额，放不下的按顺序截掉；
//   - 当前生成方式不收参考图时，角色卡整张不带（声音也不带）。
//
// used 是当前生成方式收的素材种类，limit 给出每种的上限。
func (s *GenSource) RefIDs(used []string, limit func(kind string) int) map[string][]uint64 {
	withCharacters := slices.Contains(used, KindImage)
	out := map[string][]uint64{}
	for _, kind := range []string{KindImage, KindVideo, KindAudio} {
		if !slices.Contains(used, kind) {
			continue
		}
		ids := s.refIDsOf(kind, false, nil, 0)
		if withCharacters {
			ids = s.refIDsOf(kind, true, ids, limit(kind))
		}
		if len(ids) > 0 {
			out[kind] = ids
		}
	}
	return out
}

// refIDsOf 把某种素材里普通连线（或角色卡展开）的 id 依次接到 ids 后面并去重；
// 角色卡的素材到 limit 就停，普通连线不受 limit 限制。
func (s *GenSource) refIDsOf(kind string, fromCharacter bool, ids []uint64, limit int) []uint64 {
	for _, a := range s.Assets {
		id, err := strconv.ParseUint(a.AssetID, 10, 64)
		if err != nil || a.Kind != kind || a.FromCharacter != fromCharacter || slices.Contains(ids, id) {
			continue
		}
		if fromCharacter && len(ids) >= limit {
			break
		}
		ids = append(ids, id)
	}
	return ids
}

// addCharacter 把角色卡展开成参考：主图、补充图（按保存顺序）进图片，绑定的声音进音频，规则与前端 characterRefsOfNode 一致。
// 还没有主图或正在生成的角色卡不带任何参考，但它只是少一份参考，不算「还没产物」的上游。
func (s *GenSource) addCharacter(up Node) {
	d := up.Data()
	meta, _ := d["character"].(map[string]any)
	if s.Characters == nil {
		s.Characters = map[string]string{}
	}
	if name := str(meta["name"]); name != "" {
		s.Characters[up.ID()] = name
	}
	avatar := str(d["assetId"])
	if avatar == "" || str(d["status"]) == "running" {
		return
	}
	add := func(kind, id string) {
		if id != "" {
			s.Assets = append(s.Assets, GenAsset{NodeID: up.ID(), Kind: kind, AssetID: id, FromCharacter: true})
		}
	}
	add(KindImage, avatar)
	extras, _ := meta["extras"].([]any)
	for _, e := range extras {
		if extra, ok := e.(map[string]any); ok {
			add(KindImage, str(extra["assetId"]))
		}
	}
	if voice, ok := meta["voice"].(map[string]any); ok && str(voice["mode"]) == "audio" {
		add(KindAudio, str(voice["assetId"]))
	}
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

// promptRef 匹配提示词里的素材引用 @[名字](节点id)；兼容 Agent 从用户消息里照抄的 node: 前缀写法。
var promptRef = regexp.MustCompile(`@\[([^\]\n]*)\]\((?:node:)?([^)\s]+)\)`)

// refWord 是素材引用在提示词里的叫法：「图片1」「视频2」「音频1」。
var refWord = map[string]string{"image": "图片", "video": "视频", "audio": "音频"}

// ComposePrompt 合成提交给模型的提示词，规则与前端 buildTaskInput 一致（两边共用 fixture 保证不漂移）：
//   - 节点自己的提示词里 @ 的引用先展开：文本节点换成正文，素材换成「图片1」这样的编号
//     （编号是它在 used 里同类素材中的位置，从 1 起；这种素材当前方式不带给模型，或节点已断开，就退回引用时的名字）；
//   - 没被 @ 的上游文本，按连线顺序追加在后面；提示词为空就只剩上游文本；
//   - 段与段之间空一行，只有空白的文本不收。
//
// used 是当前生成方式真正会带给模型的素材种类（见 modelcfg.RefKindsUsed）。
func ComposePrompt(s *GenSource, used []string) string {
	numbers := assetNumbers(s.Assets, used)
	texts := make(map[string]string, len(s.Texts))
	for _, t := range s.Texts {
		if text := strings.TrimSpace(t.Text); text != "" && texts[t.NodeID] == "" {
			texts[t.NodeID] = text
		}
	}
	cited := map[string]bool{}
	body := promptRef.ReplaceAllStringFunc(s.Prompt, func(m string) string {
		sub := promptRef.FindStringSubmatch(m)
		label, id := sub[1], sub[2]
		if name, ok := s.Characters[id]; ok {
			return name
		}
		if text, ok := texts[id]; ok {
			cited[id] = true
			return text
		}
		if n, ok := numbers[id]; ok {
			return n
		}
		return label
	})
	parts := []string{strings.TrimSpace(body)}
	for _, t := range s.Texts {
		if text := texts[t.NodeID]; text != "" && !cited[t.NodeID] {
			cited[t.NodeID] = true // 同一个文本节点连了两根线也只追加一次
			parts = append(parts, text)
		}
	}
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), "\n\n")
}

// assetNumbers 算每个素材节点在提示词里的叫法：同类素材按连线顺序去重后的位置。只算 used 里的种类和 id 合法的素材，
// 与 buildGenInput 放进 images / videos / audios 的内容一一对应。
func assetNumbers(assets []GenAsset, used []string) map[string]string {
	out := map[string]string{}
	// 按固定的种类顺序编号：一张角色卡同时有图和声音时，@ 它总是认图片（map 遍历顺序不定）
	for _, kind := range []string{"image", "video", "audio"} {
		word := refWord[kind]
		if !slices.Contains(used, kind) {
			continue
		}
		var ids []string
		for _, a := range assets {
			if a.Kind != kind {
				continue
			}
			if _, err := strconv.ParseUint(a.AssetID, 10, 64); err != nil {
				continue
			}
			i := slices.Index(ids, a.AssetID)
			if i < 0 {
				ids = append(ids, a.AssetID)
				i = len(ids) - 1
			}
			if _, ok := out[a.NodeID]; !ok {
				out[a.NodeID] = word + strconv.Itoa(i+1)
			}
		}
	}
	return out
}
