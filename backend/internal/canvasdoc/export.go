package canvasdoc

import (
	"fmt"
	"time"
)

// AssetEntry 是调用方查好的一个素材：内容哈希和要写进文件的元信息。
type AssetEntry struct {
	Hash string
	Info AssetInfo
}

// ExportInput 是 Build 的输入。Now、AppVersion 由调用方传入，便于测试。
type ExportInput struct {
	Title   string
	Payload []byte
	// Assets 的键是素材 ID；不在这里的素材视为读不到，导出时跳过。
	// 需要包含 CollectRefs 找到的素材（含角色节点内嵌的补充图和音频音色）。
	Assets     map[string]AssetEntry
	AppVersion string
	Now        time.Time
}

// ExportReport 是导出过程中的情况汇总。
type ExportReport struct {
	// SkippedAssets 是读不到、被跳过的不同素材 ID 个数。
	SkippedAssets int
}

// Build 把画布内容改写成导出文档：素材 ID 换成内容哈希，
// 去掉只在原环境有意义的字段（任务、src 地址、视口等），其余字段原样透传。改写规则见设计 §7.4。
// 角色数据内嵌在节点的 data.character 里，随节点导出：补充图和音频音色同样换成哈希。
//
// 读不到的素材：节点里对应的引用整体去掉（补充图去掉该项，音频音色按「无音色」导出为 null），
// 并按不同素材 ID 计入 ExportReport.SkippedAssets；旧画布遗留的 characterId / labelCache 直接丢弃。
// 画布内容解析失败、或改写后仍残留原环境字段时返回错误。
func Build(in ExportInput) (*Document, ExportReport, error) {
	// 1. 解析画布内容；顶层只取 nodes / edges，viewport 等视图状态自然丢弃
	p, err := decodePayload(in.Payload)
	if err != nil {
		return nil, ExportReport{}, err
	}

	// 2. 逐个节点就地改写。组节点没有素材和任务字段，走同一套规则也不会被改动
	e := &exporter{
		in: in,
		doc: &Document{
			Format:     Format,
			Version:    CurrentVersion,
			ExportedAt: in.Now.UTC().Format(time.RFC3339),
			AppVersion: in.AppVersion,
			Canvas:     CanvasPart{Title: in.Title, Nodes: []map[string]any{}, Edges: []map[string]any{}},
			Assets:     map[string]AssetInfo{},
		},
		skipped: map[string]bool{},
	}
	for _, node := range p.Nodes {
		// null 节点不是合法节点，丢掉，免得 Validate 拒绝整份文件
		if node == nil {
			continue
		}
		if data, ok := node["data"].(map[string]any); ok {
			e.rewriteData(data)
		}
		e.doc.Canvas.Nodes = append(e.doc.Canvas.Nodes, node)
	}
	for _, edge := range p.Edges {
		if edge != nil {
			e.doc.Canvas.Edges = append(e.doc.Canvas.Edges, edge)
		}
	}

	// 3. 扫描兜底（设计 §7.6）：以后新增的、引用原环境资源的字段若没登记改写规则，
	//    宁可导出失败也不把原账号的素材 ID / 文件地址泄出去
	if err := checkNoLeak(e.doc.Canvas); err != nil {
		return nil, ExportReport{}, err
	}
	return e.doc, ExportReport{SkippedAssets: len(e.skipped)}, nil
}

// exporter 保存一次导出过程中的状态。
type exporter struct {
	in  ExportInput
	doc *Document
	// skipped 记录读不到的素材 ID，按不同 ID 计数
	skipped map[string]bool
}

// resolve 把素材 ID 换成哈希，并把元信息登记进 doc.Assets（按哈希去重，先到先得）。
// 读不到时记入 skipped 并返回 false。
func (e *exporter) resolve(id string) (string, bool) {
	entry, ok := e.in.Assets[id]
	if !ok {
		e.skipped[id] = true
		return "", false
	}
	if _, exists := e.doc.Assets[entry.Hash]; !exists {
		e.doc.Assets[entry.Hash] = entry.Info
	}
	return entry.Hash, true
}

// resolveValue 是 resolve 的变体，接受 payload 里原样的值（字符串或 JSON 数字）；不是素材 ID 返回 false。
func (e *exporter) resolveValue(v any) (string, bool) {
	id, ok := assetIDOf(v)
	if !ok {
		return "", false
	}
	return e.resolve(id)
}

// rewriteData 按设计 §7.4 就地改写一个节点的 data。
func (e *exporter) rewriteData(data map[string]any) {
	// 1. 只在原环境有意义的字段：任务、文件地址，以及旧版角色卡节点遗留的 characterId / labelCache
	//    （角色卡已下线，这两个字段不再有任何含义）；activeOutputId 在 rewriteOutputs 里处理
	for _, key := range []string{"src", "taskId", "pendingTaskIds", "errorTaskRef", "characterId", "labelCache"} {
		delete(data, key)
	}
	// 2. 任务不会跟过来，生成中的节点导出为空闲
	if data["status"] == "running" {
		data["status"] = "idle"
	}
	// 3. 当前素材：assetId → assetHash，读不到就不带
	if v, ok := data["assetId"]; ok {
		delete(data, "assetId")
		if hash, ok := e.resolveValue(v); ok {
			data["assetHash"] = hash
		}
	}
	e.rewriteOutputs(data)
	e.rewriteParams(data)
	e.rewriteParamAssets(data)
	e.rewriteScene(data)
	e.rewriteCharacter(data)
}

// rewriteOutputs 只保留选中的那一版产物，改成 {assetHash, mediaType, model?, createdAt}；
// 那一版读不到时整个 outputs 去掉。activeOutputId 导入时重建，一并删除。
func (e *exporter) rewriteOutputs(data map[string]any) {
	kept := keptOutput(data)
	delete(data, "outputs")
	delete(data, "activeOutputId")
	if kept == nil {
		return
	}
	hash, ok := e.resolveValue(kept["assetId"])
	if !ok {
		return
	}
	out := map[string]any{"assetHash": hash}
	for _, key := range outputMetaKeys {
		if v, exists := kept[key]; exists {
			out[key] = v
		}
	}
	data["outputs"] = []any{out}
}

// outputMetaKeys 是产物里随文件导出、导入时原样带回的字段。
var outputMetaKeys = []string{"mediaType", "model", "createdAt"}

// rewriteParams 把 params.images / videos / audios 里的素材 ID 换成哈希，读不到的项去掉；其余参数原样。
func (e *exporter) rewriteParams(data map[string]any) {
	params, _ := data["params"].(map[string]any)
	for _, key := range mediaParamKeys {
		list, ok := params[key].([]any)
		if !ok {
			continue
		}
		hashes := make([]any, 0, len(list))
		for _, v := range list {
			if hash, ok := e.resolveValue(v); ok {
				hashes = append(hashes, hash)
			}
		}
		params[key] = hashes
	}
}

// rewriteParamAssets 把 paramAssets 的键从素材 ID 换成哈希，值去掉 url（导入时按新素材重建），
// 保留 fileName、mediaType 等展示信息；读不到的项去掉。
func (e *exporter) rewriteParamAssets(data map[string]any) {
	paramAssets, ok := data["paramAssets"].(map[string]any)
	if !ok {
		return
	}
	rewritten := make(map[string]any, len(paramAssets))
	for _, id := range sortedKeys(paramAssets) {
		hash, ok := e.resolve(id)
		if !ok {
			continue
		}
		value := map[string]any{}
		if m, ok := paramAssets[id].(map[string]any); ok {
			for k, v := range m {
				if k != "url" {
					value[k] = v
				}
			}
		}
		// 不同素材 ID 内容相同时哈希相同，保留排序在前的那项
		if _, exists := rewritten[hash]; !exists {
			rewritten[hash] = value
		}
	}
	data["paramAssets"] = rewritten
}

// rewriteScene 把导演台缩略图 scene.thumb {assetId, url} 换成 scene.thumbHash，场景其余内容原样。
func (e *exporter) rewriteScene(data map[string]any) {
	scene, ok := data["scene"].(map[string]any)
	if !ok {
		return
	}
	thumb, exists := scene["thumb"]
	if !exists {
		return
	}
	delete(scene, "thumb")
	m, _ := thumb.(map[string]any)
	if hash, ok := e.resolveValue(m["assetId"]); ok {
		scene["thumbHash"] = hash
	}
}

// rewriteCharacter 改写角色节点内嵌的 data.character：补充图 extras[] 从 {assetId, src} 换成 {assetHash}，
// 读不到的项去掉；音频音色 {mode:"audio", assetId, src, origin} 换成 {mode, assetHash, origin}，
// 读不到（或缺 assetId）时音色按「无音色」导出为 null；文本音色和其余角色字段原样。
// 主图沿用 data.assetId 的规则，不在这里。
func (e *exporter) rewriteCharacter(data map[string]any) {
	character, ok := data["character"].(map[string]any)
	if !ok {
		return
	}
	if extras, ok := character["extras"].([]any); ok {
		kept := make([]any, 0, len(extras))
		for _, item := range extras {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if hash, ok := e.resolveValue(m["assetId"]); ok {
				kept = append(kept, map[string]any{"assetHash": hash})
			}
		}
		character["extras"] = kept
	}
	if voice, ok := character["voice"].(map[string]any); ok && voice["mode"] == voiceModeAudio {
		hash, ok := e.resolveValue(voice["assetId"])
		if !ok {
			character["voice"] = nil
			return
		}
		out := map[string]any{"mode": voiceModeAudio, "assetHash": hash}
		if origin, exists := voice["origin"]; exists {
			out["origin"] = origin
		}
		character["voice"] = out
	}
}

// leakKeys 是改写后不应再出现在节点和连线任何层级里的字段名：它们携带原环境的素材 ID、文件地址或任务 ID。
var leakKeys = map[string]bool{
	"assetId": true, "src": true, "url": true, "taskId": true, "pendingTaskIds": true,
	"errorTaskRef": true, "characterId": true, "activeOutputId": true, "labelCache": true,
}

// checkNoLeak 递归扫描改写后的节点和连线，发现 leakKeys 里的字段就返回错误。
func checkNoLeak(c CanvasPart) error {
	for _, list := range [][]map[string]any{c.Nodes, c.Edges} {
		for _, item := range list {
			if key, found := findKey(item, leakKeys); found {
				id, _ := item["id"].(string)
				return fmt.Errorf("导出画布: %q 里残留原环境字段 %q，需要登记改写规则", id, key)
			}
		}
	}
	return nil
}

// findKey 在任意 JSON 值里查找 keys 中的字段名。同层的键按字典序检查，同一份内容总是报同一个字段。
func findKey(v any, keys map[string]bool) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(x) {
			if keys[k] {
				return k, true
			}
			if key, found := findKey(x[k], keys); found {
				return key, true
			}
		}
	case []any:
		for _, child := range x {
			if key, found := findKey(child, keys); found {
				return key, true
			}
		}
	}
	return "", false
}
