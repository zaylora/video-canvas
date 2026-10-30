package canvasgraph

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// GenSource 是一个节点发起生成所需的全部输入，从画布里读出来：节点自己的模型、提示词、参数，
// 以及连进来的上游文字和素材。组装成任务输入、校验和计价由 service 层按模型能力完成。
type GenSource struct {
	NodeID     string         // 节点 id
	Kind       string         // script / image / video / audio
	Label      string         // 标题
	Model      string         // 节点上选的模型 key，没选为空
	Prompt     string         // 节点自己的提示词
	Params     map[string]any // 节点的生成参数（不含素材引用）
	Texts      []GenText      // 上游文本节点的正文，按连线顺序，空文本不收
	Assets     []GenAsset     // 上游已经有产物的素材：普通连线按连线顺序在前，角色卡展开的素材在后
	Characters []GenCharacter // 角色按连线顺序收集；保留主图和声音关系，不用名字作为身份
	Pending    int            // 上游里还没有产物的素材节点数，只作提示
	Status     string         // 节点当前状态
	HasOutput  bool           // 节点是否已经有产物：生成不会覆盖已有产物
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
	CharacterPart string // avatar / extra / voice；普通素材为空
}

// GenCharacter 是一次生成引用的角色；没有主图时仍保留声音关系，避免静默丢弃声音。
type GenCharacter struct {
	NodeID, Name, AvatarAssetID, VoiceAssetID string
	VoiceTaskID                               string // 非空时提交前核对任务状态，不沿用生成中的旧声音
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
//   - 视频先保留所有角色主图和声音，再按剩余名额填补充图；必要素材超限由任务校验报错；
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
		for _, id := range s.manualRefIDs(kind) {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if withCharacters {
			ids = s.refIDsOf(kind, true, ids, limit(kind))
		}
		if len(ids) > 0 {
			out[kind] = ids
		}
	}
	return out
}

// refIDsOf 把素材依次去重；视频先保留必要主图和声音，补充图才受剩余名额限制。
func (s *GenSource) refIDsOf(kind string, fromCharacter bool, ids []uint64, limit int) []uint64 {
	assets := append([]GenAsset(nil), s.Assets...)
	if s.Kind == KindVideo && fromCharacter {
		slices.SortStableFunc(assets, func(a, b GenAsset) int {
			if (a.CharacterPart == "extra") == (b.CharacterPart == "extra") {
				return 0
			}
			if a.CharacterPart == "extra" {
				return 1
			}
			return -1
		})
	}
	for _, a := range assets {
		id, err := strconv.ParseUint(a.AssetID, 10, 64)
		if err != nil || a.Kind != kind || a.FromCharacter != fromCharacter || slices.Contains(ids, id) {
			continue
		}
		if fromCharacter && len(ids) >= limit && (s.Kind != KindVideo || a.CharacterPart == "extra") {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// manualRefIDs 读取节点手动添加的素材，和连线素材一起去重后决定最终编号。
func (s *GenSource) manualRefIDs(kind string) []uint64 {
	raw, err := json.Marshal(s.Params[kind+"s"])
	if err != nil {
		return nil
	}
	var values []json.Number
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	var ids []uint64
	for _, value := range values {
		if id, err := strconv.ParseUint(string(value), 10, 64); err == nil && id > 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// addCharacter 把角色卡展开成参考：主图、补充图（按保存顺序）进图片，绑定的声音进音频，规则与前端 characterRefsOfNode 一致。
// 主图未就绪时保留声音关系供校验，不带旧图；没有声音的空卡仍只退回人物名字。
func (s *GenSource) addCharacter(up Node) {
	d := up.Data()
	meta, _ := d["character"].(map[string]any)
	if slices.ContainsFunc(s.Characters, func(c GenCharacter) bool { return c.NodeID == up.ID() }) {
		return
	}
	role := GenCharacter{NodeID: up.ID(), Name: str(meta["name"]), VoiceTaskID: str(d["voiceTaskId"])}
	if role.Name == "" {
		role.Name = up.Label()
	}
	if voice, ok := meta["voice"].(map[string]any); ok && str(voice["mode"]) == "audio" {
		role.VoiceAssetID = str(voice["assetId"])
	}
	avatar := str(d["assetId"])
	if avatar == "" || str(d["status"]) == "running" {
		s.Characters = append(s.Characters, role)
		return
	}
	role.AvatarAssetID = avatar
	s.Characters = append(s.Characters, role)
	add := func(kind, id, part string) {
		if id != "" {
			s.Assets = append(s.Assets, GenAsset{NodeID: up.ID(), Kind: kind, AssetID: id, FromCharacter: true, CharacterPart: part})
		}
	}
	add(KindImage, avatar, "avatar")
	extras, _ := meta["extras"].([]any)
	for _, e := range extras {
		if extra, ok := e.(map[string]any); ok {
			add(KindImage, str(extra["assetId"]), "extra")
		}
	}
	if voice, ok := meta["voice"].(map[string]any); ok && str(voice["mode"]) == "audio" {
		add(KindAudio, str(voice["assetId"]), "voice")
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

// ComposePrompt 按最终参考素材编号展开 @，追加上游文字及视频人物与音色对应说明。
// refs 必须是实际提交的素材数组，不能用截断、去重前的素材估算编号。
func ComposePrompt(s *GenSource, refs map[string][]uint64) string {
	numbers := assetNumbers(s.Assets, refs)
	names, blocks := s.characterPrompt(refs)
	texts := make(map[string]string, len(s.Texts))
	for _, t := range s.Texts {
		if text := strings.TrimSpace(t.Text); text != "" && texts[t.NodeID] == "" {
			texts[t.NodeID] = text
		}
	}
	for _, c := range s.Characters {
		if names[c.NodeID] == "" {
			names[c.NodeID] = c.Name
		}
	}
	for id, text := range texts {
		texts[id] = expandMediaPrompt(text, names, numbers)
	}
	cited := map[string]bool{}
	body := promptRef.ReplaceAllStringFunc(s.Prompt, func(m string) string {
		id := promptRef.FindStringSubmatch(m)[2]
		if text, ok := texts[id]; ok {
			cited[id] = true
			return text
		}
		return expandMediaPrompt(m, names, numbers)
	})
	parts := []string{strings.TrimSpace(body)}
	for _, t := range s.Texts {
		if text := texts[t.NodeID]; text != "" && !cited[t.NodeID] {
			cited[t.NodeID] = true
			parts = append(parts, text)
		}
	}
	parts = slices.DeleteFunc(parts, func(p string) bool { return p == "" })
	if len(parts) == 0 {
		return ""
	}
	parts = append(parts, blocks...)
	return strings.Join(parts, "\n\n")
}

// expandMediaPrompt 只展开人物和媒体引用，不递归展开文本节点，避免剧本之间相互引用形成循环。
func expandMediaPrompt(text string, names, numbers map[string]string) string {
	return promptRef.ReplaceAllStringFunc(text, func(m string) string {
		sub := promptRef.FindStringSubmatch(m)
		if name, ok := names[sub[2]]; ok {
			return name
		}
		if number, ok := numbers[sub[2]]; ok {
			return number
		}
		return sub[1]
	})
}

// assetNumbers 按最终数组计算同类素材编号；同一份素材被多个节点引用时复用编号。
func assetNumbers(assets []GenAsset, refs map[string][]uint64) map[string]string {
	out := map[string]string{}
	for _, kind := range []string{KindImage, KindVideo, KindAudio} {
		for _, a := range assets {
			if a.Kind != kind {
				continue
			}
			index := referenceIndex(refs, kind, a.AssetID)
			if index >= 0 && out[a.NodeID] == "" {
				out[a.NodeID] = refWord[kind] + strconv.Itoa(index+1)
			}
		}
	}
	return out
}

// referenceIndex 查素材在实际提交数组中的位置；没提交或 id 不合法返回 -1。
func referenceIndex(refs map[string][]uint64, kind, assetID string) int {
	id, err := strconv.ParseUint(assetID, 10, 64)
	if err != nil || id == 0 {
		return -1
	}
	return slices.Index(refs[kind], id)
}

// characterPrompt 按角色节点编译人物及声音说明，只用于视频，不注入人物设定或试音台词。
func (s *GenSource) characterPrompt(refs map[string][]uint64) (map[string]string, []string) {
	names := map[string]string{}
	if s.Kind != KindVideo {
		return names, nil
	}
	var visuals, voices, blocks []string
	for _, c := range s.Characters {
		image := referenceIndex(refs, KindImage, c.AvatarAssetID)
		if image < 0 {
			continue
		}
		subject := fmt.Sprintf("%s（图片%d中的人物）", c.Name, image+1)
		names[c.NodeID] = subject
		var extras []string
		for _, a := range s.Assets {
			if a.NodeID != c.NodeID || a.CharacterPart != "extra" {
				continue
			}
			index := referenceIndex(refs, KindImage, a.AssetID)
			label := fmt.Sprintf("图片%d", index+1)
			if index >= 0 && index != image && !slices.Contains(extras, label) {
				extras = append(extras, label)
			}
		}
		line := fmt.Sprintf("%s：图片%d中的人物", c.Name, image+1)
		if len(extras) > 0 {
			line += "，补充视角：" + strings.Join(extras, "、")
		}
		visuals = append(visuals, line+"。")
		if audio := referenceIndex(refs, KindAudio, c.VoiceAssetID); audio >= 0 {
			voices = append(voices, fmt.Sprintf("%s说话时，音色参考音频%d。", subject, audio+1))
		}
	}
	if len(visuals) > 0 {
		blocks = append(blocks, "【人物参考】\n"+strings.Join(visuals, "\n"))
	}
	if len(voices) > 0 {
		blocks = append(blocks, "【人物音色对应】\n"+strings.Join(voices, "\n")+"\n角色声音样本只用于音色参考，本次台词以剧情为准。")
	}
	return names, blocks
}

// ValidateCharacterInput 检查视频中已绑定声音的角色是否有完整的主图与音频，防止降级后继续提交。
func (s *GenSource) ValidateCharacterInput(refs map[string][]uint64, generateAudio bool) error {
	if s.Kind != KindVideo {
		return nil
	}
	for _, c := range s.Characters {
		if c.VoiceAssetID == "" {
			continue
		}
		if referenceIndex(refs, KindImage, c.AvatarAssetID) < 0 {
			return fmt.Errorf("角色「%s」的主图未就绪或当前方式不支持参考图片", c.Name)
		}
		if referenceIndex(refs, KindAudio, c.VoiceAssetID) < 0 {
			return fmt.Errorf("角色「%s」的声音未带入，请选择支持参考音频的生成方式", c.Name)
		}
		if !generateAudio {
			return fmt.Errorf("使用角色音色时请开启声音生成")
		}
	}
	return nil
}
