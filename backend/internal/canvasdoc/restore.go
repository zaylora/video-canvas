package canvasdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// RestoredAsset 是导入端为一个哈希新建好的素材。
type RestoredAsset struct {
	// ID 是新素材 ID（十进制字符串），原样作为 JSON 字符串写进画布内容。
	ID string
	// URL 是新素材的访问地址，用于重建 src、paramAssets.url 和 scene.thumb.url。
	URL string
}

// RestoreInput 是 Restore 的输入。
type RestoreInput struct {
	// Doc 是已通过 Validate 的导入文档，Restore 不会改动它。
	Doc *Document
	// Assets 的键是文档里的素材哈希；不在这里的哈希视为复制失败。
	Assets map[string]RestoredAsset
}

// MissingAsset 是一个节点里没能还原的素材。
type MissingAsset struct {
	// NodeID 是节点 ID；文件里节点缺 id 时为空串。
	NodeID string
	// FileName 取自文档 assets 里该哈希的文件名。
	FileName string
}

// RestoreReport 是导入还原过程中的情况汇总。
type RestoreReport struct {
	// MissingAssets 按节点顺序列出缺失的素材；同一节点里同一哈希只记一次。
	MissingAssets []MissingAsset
	// EdgesDropped 是因 source / target 指向不存在的节点而丢弃的连线数。
	EdgesDropped int
}

// restoredCanvas 是还原出的画布内容，形状与前端 serializeGraph 一致。
type restoredCanvas struct {
	Nodes    []map[string]any `json:"nodes"`
	Edges    []map[string]any `json:"edges"`
	Viewport viewport         `json:"viewport"`
}

// viewport 是画布视口。文件不带视口（设计 §7.6），导入的画布一律写默认值。
type viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

// Restore 是 Build 的逆过程：把导入文档还原成画布内容（payload_json）。
// 素材哈希换成新素材 ID 并重建 src、outputs、activeOutputId、paramAssets.url、scene.thumb，
// 角色节点内嵌的补充图和音频音色同样换成新素材 ID 与地址；
// 节点和连线的其余字段、顺序原样，组节点原样，视口写默认值。改写规则见设计 §6.3、§7.4。
//
// 哈希不在 in.Assets 里：去掉节点里对应的引用（主素材缺失时连同 src、outputs、activeOutputId 一起去掉；
// 补充图去掉该项，音频音色变成 null），节点保留，记入 RestoreReport.MissingAssets；
// source / target 指向不存在节点的连线丢弃，计入 EdgesDropped。
// in.Doc 为 nil 或序列化失败时返回错误。
func Restore(in RestoreInput) ([]byte, RestoreReport, error) {
	if in.Doc == nil {
		return nil, RestoreReport{}, errors.New("还原画布: 文档为空")
	}
	// 1. 深拷贝节点和连线后就地改写，调用方手里的文档不受影响
	var c CanvasPart
	if err := cloneJSON(in.Doc.Canvas, &c); err != nil {
		return nil, RestoreReport{}, fmt.Errorf("还原画布: %w", err)
	}

	// 2. 逐个节点改写。组节点没有素材字段，原样保留
	r := &restorer{in: in}
	out := restoredCanvas{Nodes: []map[string]any{}, Edges: []map[string]any{}, Viewport: viewport{Zoom: 1}}
	nodeIDs := map[string]bool{}
	for _, node := range c.Nodes {
		if node == nil {
			continue
		}
		id, _ := node["id"].(string)
		if id != "" {
			nodeIDs[id] = true
		}
		if node["type"] != "group" {
			r.restoreNode(id, node)
		}
		out.Nodes = append(out.Nodes, node)
	}

	// 3. 连线两端都必须是还原后存在的节点
	for _, edge := range c.Edges {
		source, _ := edge["source"].(string)
		target, _ := edge["target"].(string)
		if !nodeIDs[source] || !nodeIDs[target] {
			r.rep.EdgesDropped++
			continue
		}
		out.Edges = append(out.Edges, edge)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, RestoreReport{}, fmt.Errorf("还原画布: %w", err)
	}
	return raw, r.rep, nil
}

// cloneJSON 经一次 JSON 往返把 src 深拷贝进 dst，数字用 json.Number 保持原样。
func cloneJSON(src, dst any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(dst)
}

// restorer 保存一次还原过程中的状态。
type restorer struct {
	in  RestoreInput
	rep RestoreReport
	// nodeID、reported 只在还原当前节点时有效：reported 记录本节点已报告缺失的哈希，同一哈希只报一次
	nodeID   string
	reported map[string]bool
}

// lookup 查哈希对应的新素材。v 不是字符串（不是哈希引用）时返回 false 且不报告；
// 是哈希但不在 in.Assets 里时记入缺失报告并返回 false。
func (r *restorer) lookup(v any) (RestoredAsset, bool) {
	hash, ok := v.(string)
	if !ok {
		return RestoredAsset{}, false
	}
	if a, ok := r.in.Assets[hash]; ok {
		return a, true
	}
	if !r.reported[hash] {
		r.reported[hash] = true
		r.rep.MissingAssets = append(r.rep.MissingAssets, MissingAsset{
			NodeID: r.nodeID, FileName: r.in.Doc.Assets[hash].FileName,
		})
	}
	return RestoredAsset{}, false
}

// restoreNode 按设计 §6.3 就地还原一个节点的 data；data 不是对象时不动。
func (r *restorer) restoreNode(id string, node map[string]any) {
	data, ok := node["data"].(map[string]any)
	if !ok {
		return
	}
	r.nodeID = id
	r.reported = map[string]bool{}

	// 1. 合法文件不会带原环境字段；手改的文件若带了，先清掉，免得节点指向别人的素材或任务
	for key := range leakKeys {
		delete(data, key)
	}
	// 2. 当前素材：assetHash → assetId + src
	mainMissing := false
	if v, exists := data["assetHash"]; exists {
		delete(data, "assetHash")
		if a, ok := r.lookup(v); ok {
			data["assetId"] = a.ID
			data["src"] = a.URL
		} else {
			mainMissing = true
		}
	}
	r.restoreOutputs(data, mainMissing)
	r.restoreParams(data)
	r.restoreParamAssets(data)
	r.restoreScene(data)
	r.restoreCharacter(data)
	// 角色卡下线前导出的旧文件里节点带 characterRef，已没有对应的角色字典，只丢弃这个引用
	delete(data, "characterRef")
}

// restoreOutputs 把产物还原成 {id, src, assetId, mediaType, model?, createdAt}，id 取新素材 ID，
// activeOutputId 设为最后一版（导出只保留一版，即唯一那版）。主素材缺失时 src 无从镜像，产物整体去掉。
func (r *restorer) restoreOutputs(data map[string]any, mainMissing bool) {
	outputs, _ := data["outputs"].([]any)
	delete(data, "outputs")
	restored := make([]any, 0, len(outputs))
	for _, o := range outputs {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		a, ok := r.lookup(m["assetHash"])
		if !ok {
			continue
		}
		out := map[string]any{"id": a.ID, "src": a.URL, "assetId": a.ID}
		for _, key := range outputMetaKeys {
			if v, exists := m[key]; exists {
				out[key] = v
			}
		}
		restored = append(restored, out)
	}
	if mainMissing || len(restored) == 0 {
		return
	}
	data["outputs"] = restored
	data["activeOutputId"] = restored[len(restored)-1].(map[string]any)["id"]
}

// restoreParams 把 params.images / videos / audios 里的哈希换成新素材 ID，缺失或不是哈希的项去掉；
// 这几个参数不是数组时整项去掉（只能是手改出来的原环境值）。其余参数原样。
func (r *restorer) restoreParams(data map[string]any) {
	params, _ := data["params"].(map[string]any)
	for _, key := range mediaParamKeys {
		v, exists := params[key]
		if !exists {
			continue
		}
		list, ok := v.([]any)
		if !ok {
			delete(params, key)
			continue
		}
		ids := make([]any, 0, len(list))
		for _, item := range list {
			if a, ok := r.lookup(item); ok {
				ids = append(ids, a.ID)
			}
		}
		params[key] = ids
	}
}

// restoreParamAssets 把 paramAssets 的键从哈希换成新素材 ID，值按新素材重建 url，保留 fileName、mediaType 等展示信息；
// 缺失的项去掉。
func (r *restorer) restoreParamAssets(data map[string]any) {
	v, exists := data["paramAssets"]
	if !exists {
		return
	}
	paramAssets, _ := v.(map[string]any)
	rewritten := make(map[string]any, len(paramAssets))
	for _, hash := range sortedKeys(paramAssets) {
		a, ok := r.lookup(hash)
		if !ok {
			continue
		}
		value := map[string]any{}
		if m, ok := paramAssets[hash].(map[string]any); ok {
			for k, field := range m {
				value[k] = field
			}
		}
		value["url"] = a.URL
		// 不同哈希被映射到同一个新素材时保留排序在前的那项，与 Build 一致
		if _, exists := rewritten[a.ID]; !exists {
			rewritten[a.ID] = value
		}
	}
	data["paramAssets"] = rewritten
}

// restoreScene 把导演台的 scene.thumbHash 换回 scene.thumb {assetId, url}，场景其余内容原样。
func (r *restorer) restoreScene(data map[string]any) {
	scene, ok := data["scene"].(map[string]any)
	if !ok {
		return
	}
	delete(scene, "thumb")
	v, exists := scene["thumbHash"]
	if !exists {
		return
	}
	delete(scene, "thumbHash")
	if a, ok := r.lookup(v); ok {
		scene["thumb"] = map[string]any{"assetId": a.ID, "url": a.URL}
	}
}

// restoreCharacter 还原角色节点内嵌的 data.character：补充图 {assetHash} 换回 {assetId, src}，缺失的项去掉；
// 音频音色 {mode:"audio", assetHash, origin} 换回 {mode, assetId, src, origin}，缺失时变成 null；
// 文本音色和其余角色字段原样。
func (r *restorer) restoreCharacter(data map[string]any) {
	character, ok := data["character"].(map[string]any)
	if !ok {
		return
	}
	if extras, ok := character["extras"].([]any); ok {
		restored := make([]any, 0, len(extras))
		for _, item := range extras {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if a, ok := r.lookup(m["assetHash"]); ok {
				restored = append(restored, map[string]any{"assetId": a.ID, "src": a.URL})
			}
		}
		character["extras"] = restored
	}
	if voice, ok := character["voice"].(map[string]any); ok && voice["mode"] == voiceModeAudio {
		a, ok := r.lookup(voice["assetHash"])
		if !ok {
			character["voice"] = nil
			return
		}
		out := map[string]any{"mode": voiceModeAudio, "assetId": a.ID, "src": a.URL}
		if origin, exists := voice["origin"]; exists {
			out["origin"] = origin
		}
		character["voice"] = out
	}
}
