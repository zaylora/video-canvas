package canvasgraph

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// DramaAsset 是拆解产生的资产：身份（类型、名称、造型、别名、集号）用于合并，
// Description / VoicePrompt 只在新建角色节点时写入节点内嵌的 character 元数据。
type DramaAsset struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Look        string   `json:"look"`
	Aliases     []string `json:"aliases,omitempty"`
	Episodes    []int    `json:"episodes"`
	Description string   `json:"description,omitempty"`
	VoicePrompt string   `json:"voice_prompt,omitempty"`
}

// defaultCharacterLook 是角色节点没有形象名时的默认形象名，与前端空节点默认值一致。
const defaultCharacterLook = "基础形象"

// DramaNode 标识本次新建节点与原始资产下标，供调用方继续生成提示词。
type DramaNode struct {
	NodeID     string
	AssetIndex int
}

// DramaResult 包含公共画布差异和仅本次新增的资产节点。
type DramaResult struct {
	*Result
	Created []DramaNode
}

// MergeDrama 按类型、名称和造型或别名合并资产，只给已有节点并集号，未分类节点原样保留。
func MergeDrama(g *Graph, assets []DramaAsset, opt Options) (*DramaResult, error) {
	after := g.Clone()
	created := []DramaNode{}
	// 索引已有身份与本批别名；每个已有节点只解析一次，不在资产匹配循环中反复序列化 JSON。
	index := make(map[dramaIdentity]Node, len(after.Nodes)+len(assets))
	for _, n := range after.Nodes {
		if meta, ok := dramaMeta(n); ok {
			indexDramaNames(index, meta, n)
		}
	}
	for i, asset := range assets {
		asset.Name, asset.Look = strings.TrimSpace(asset.Name), strings.TrimSpace(asset.Look)
		if err := checkDramaAsset(asset, i); err != nil {
			return nil, err
		}
		if found := findDramaNode(index, asset); found != nil {
			meta, _ := dramaMeta(found)
			meta.Episodes = unionEpisodes(meta.Episodes, asset.Episodes)
			// 只改集号，保留前端、用户或上一轮生成的其他 metadata 字段和提示词。
			d := found.Data()["drama"].(map[string]any)
			d["episodes"] = episodeValues(meta.Episodes)
			indexDramaNames(index, asset, found)
			continue
		}
		n := newDramaNode(opt, asset)
		n.SetPosition(dramaPosition(after, asset.Type, n))
		after.Nodes = append(after.Nodes, n)
		indexDramaNames(index, asset, n)
		created = append(created, DramaNode{NodeID: n.ID(), AssetIndex: i})
	}
	return &DramaResult{Result: &Result{Graph: after, Changes: Diff(g, after)}, Created: created}, nil
}

// dramaType 只接受契约中的三种资产分类。
func dramaType(kind string) bool { return kind == "character" || kind == "scene" || kind == "prop" }

// dramaKey 按契约忽略首尾空白与大小写，不改变名称内部空格。
func dramaKey(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

// dramaIdentity 是类型和造型范围内的规范名称，结构体键避免拼接身份字符串。
type dramaIdentity struct{ kind, look, name string }

// indexDramaNames 记录名称和别名，已有冲突身份保持最早节点，不能删除或覆盖用户已有资产。
func indexDramaNames(index map[dramaIdentity]Node, a DramaAsset, n Node) {
	key := dramaIdentity{kind: a.Type, look: dramaKey(a.Look), name: dramaKey(a.Name)}
	if index[key] == nil {
		index[key] = n
	}
	for _, alias := range a.Aliases {
		key.name = dramaKey(alias)
		if key.name != "" && index[key] == nil {
			index[key] = n
		}
	}
}

// dramaMeta 只读取合法分类，未分类和前端遗留的异常字段不参与合并。
func dramaMeta(n Node) (DramaAsset, bool) {
	d, _ := n["data"].(map[string]any)
	raw, ok := d["drama"].(map[string]any)
	if !ok {
		return DramaAsset{}, false
	}
	a := DramaAsset{Type: str(raw["type"]), Name: str(raw["name"]), Look: str(raw["look"])}
	if !dramaType(a.Type) || dramaKey(a.Name) == "" {
		return DramaAsset{}, false
	}
	switch episodes := raw["episodes"].(type) {
	case nil:
		// null：资产所在的集被删了、还没重新归类；身份仍可参与合并，合并后归到新集
		a.Episodes = nil
	case []any:
		a.Episodes = make([]int, len(episodes))
		for i, value := range episodes {
			no, ok := value.(float64)
			if !ok || no < 1 || no > 100 || math.Trunc(no) != no {
				return DramaAsset{}, false
			}
			a.Episodes[i] = int(no)
		}
	case []int:
		a.Episodes = episodes
	default:
		return DramaAsset{}, false
	}
	switch aliases := raw["aliases"].(type) {
	case []any:
		for _, alias := range aliases {
			if name := str(alias); name != "" {
				a.Aliases = append(a.Aliases, name)
			}
		}
	case []string:
		a.Aliases = aliases
	}
	return a, true
}

// RemoveDramaEpisode 删掉第 no 集后改写资产的出现集数：去掉 no，后面的集号减 1；
// 原来只出现在这一集的资产改成 null（未分类，保留类型和名字），不能变成 [] 被当成全集；全集保持不变。
// 没有短剧元数据或集数不是数组的节点原样保留，入参不变。
func RemoveDramaEpisode(g *Graph, no int) *Result {
	after := g.Clone()
	for _, n := range after.Nodes {
		d, _ := n["data"].(map[string]any)
		meta, ok := d["drama"].(map[string]any)
		if !ok {
			continue
		}
		episodes, ok := meta["episodes"].([]any)
		if !ok || len(episodes) == 0 {
			continue
		}
		next := make([]any, 0, len(episodes))
		for _, value := range episodes {
			ep, ok := value.(float64)
			switch {
			case !ok:
				next = append(next, value)
			case int(ep) == no:
			case int(ep) > no:
				next = append(next, ep-1)
			default:
				next = append(next, ep)
			}
		}
		if len(next) == 0 {
			meta["episodes"] = nil
			continue
		}
		meta["episodes"] = next
	}
	return &Result{Graph: after, Changes: Diff(g, after)}
}

// unionEpisodes 空数组代表全集，不能在合并时被缩窄成一部分集。
func unionEpisodes(a, b []int) []int {
	// nil 是未分类（不属于任何集），不能和 [] 的「全集」混在一起：并上一方等于直接取另一方
	if a == nil {
		return append([]int{}, b...)
	}
	if len(a) == 0 || len(b) == 0 {
		return []int{}
	}
	set := make(map[int]bool, len(a)+len(b))
	for _, no := range a {
		set[no] = true
	}
	for _, no := range b {
		set[no] = true
	}
	out := make([]int, 0, len(set))
	for no := range set {
		out = append(out, no)
	}
	sort.Ints(out)
	return out
}

// episodeValues 使用 JSON 原生切片，避免 Clone 后差异比较出现 []int 与 []any 的无效改动。
func episodeValues(nos []int) []any {
	out := make([]any, len(nos))
	for i, no := range nos {
		out[i] = float64(no)
	}
	return out
}

// dramaPosition 追加在同类型行末尾，仅移动新节点，使用现有节点的实际画幅与组坐标避让。
func dramaPosition(g *Graph, kind string, n Node) Point {
	p := Point{}
	switch kind {
	case "scene":
		p.Y = 800
	case "prop":
		p.Y = 1600
	}
	hasRow := false
	for _, existing := range g.Nodes {
		data, _ := existing["data"].(map[string]any)
		meta, _ := data["drama"].(map[string]any)
		if str(meta["type"]) != kind || dramaKey(str(meta["name"])) == "" {
			continue
		}
		r := g.absRect(existing)
		if !hasRow {
			p.Y = r.y
			hasRow = true
		}
		p.X = math.Max(p.X, r.x+r.w+defaultGapX)
	}
	w, h := nodeSize(n)
	// 单向向右扫描；每次碰撞至少越过一个旧节点右边界，保证终止并且不碰未分类节点。
	for {
		next := p.X
		for _, existing := range g.Nodes {
			r := g.absRect(existing)
			if p.X < r.x+r.w+defaultGapX && p.X+w+defaultGapX > r.x && p.Y < r.y+r.h+defaultGapY && p.Y+h+defaultGapY > r.y {
				next = math.Max(next, r.x+r.w+defaultGapX)
			}
		}
		if next == p.X {
			return p
		}
		p.X = next
	}
}

// ApplyDramaPrompts 只给已分类资产写提示词；任何目标无效时整批不生效，保留其他参数。
func ApplyDramaPrompts(g *Graph, prompts map[string]string) (*Result, error) {
	after := g.Clone()
	for id, prompt := range prompts {
		n := after.Node(id)
		if n == nil {
			return nil, fmt.Errorf("%w: 节点 %q 不存在", ErrInvalid, id)
		}
		if _, ok := dramaMeta(n); !ok {
			return nil, fmt.Errorf("%w: 节点 %q 不是短剧资产", ErrInvalid, id)
		}
		setPrompt(n, prompt)
	}
	return &Result{Graph: after, Changes: Diff(g, after)}, nil
}

// checkDramaAsset 校验资产分类、名称与集号，i 用于报错定位。
func checkDramaAsset(asset DramaAsset, i int) error {
	if !dramaType(asset.Type) || asset.Name == "" {
		return fmt.Errorf("%w: 第 %d 项资产类型或名称不合法", ErrInvalid, i+1)
	}
	for _, no := range asset.Episodes {
		if no < 1 {
			return fmt.Errorf("%w: 集号必须大于零", ErrInvalid)
		}
	}
	return nil
}

// findDramaNode 先按名称、再按别名查已有节点；找不到返回 nil。
func findDramaNode(index map[dramaIdentity]Node, asset DramaAsset) Node {
	key := dramaIdentity{kind: asset.Type, look: dramaKey(asset.Look), name: dramaKey(asset.Name)}
	if found := index[key]; found != nil {
		return found
	}
	for _, alias := range asset.Aliases {
		key.name = dramaKey(alias)
		if found := index[key]; found != nil {
			return found
		}
	}
	return nil
}

// newDramaNode 按契约 §4 构造新资产节点（位置由调用方设置）。
func newDramaNode(opt Options, asset DramaAsset) Node {
	kind := KindImage
	if asset.Type == "character" {
		kind = KindCharacter
	}
	label := asset.Name
	if asset.Look != "" {
		label += " · " + asset.Look
	}
	n := Node{"id": opt.newID("n_"), "type": "canvas", "data": map[string]any{
		"kind": kind, "label": label, "drama": map[string]any{
			"type": asset.Type, "name": asset.Name, "look": asset.Look, "episodes": episodeValues(unionEpisodes(asset.Episodes, asset.Episodes)),
		},
	}}
	if kind == KindCharacter {
		n.Data()["character"] = newCharacterMeta(asset)
	}
	return n
}

// newCharacterMeta 构造角色节点内嵌的 character 元数据：角色数据只属于当前画布，
// 主图与补充图在拆解时为空（extras 为 []）。声音只有音频一种，拆解不会生成音频，所以 voice 恒为 null；
// 拆解给出的音色描述写在 voicePrompt，作为用户日后在详情页生成声音时的提示词（没有描述就不写这个键）。
// 全部使用 JSON 原生类型（[]any / map[string]any），避免 Clone 后差异比较出现无效改动。
func newCharacterMeta(asset DramaAsset) map[string]any {
	look := asset.Look
	if look == "" {
		look = defaultCharacterLook
	}
	meta := map[string]any{
		"name": asset.Name, "look": look, "description": strings.TrimSpace(asset.Description),
		"extras": []any{}, "voice": nil,
	}
	if text := strings.TrimSpace(asset.VoicePrompt); text != "" {
		meta["voicePrompt"] = text
	}
	return meta
}
