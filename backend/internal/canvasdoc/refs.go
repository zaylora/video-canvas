package canvasdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// 本文件集中登记画布内容（payload_json）里引用原环境素材的位置。
// 以后给节点新增引用素材的字段，必须同时改这里的 assetIDRefs 和 export.go 的改写，
// 否则原账号的素材 ID 会泄进导出文件（设计 §8.4）。

// Refs 是一张画布内容引用到的素材和角色卡，都去重并按首次出现的顺序排列。
type Refs struct {
	// AssetIDs 是素材 ID（十进制字符串）。
	AssetIDs []string
	// CharacterIDs 是角色卡 ID（十六进制字符串）。
	CharacterIDs []string
}

// payload 是画布内容里导出关心的部分；viewport 等其余顶层字段不读，也就不会进导出文件。
type payload struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

// decodePayload 解析画布内容。用 UseNumber 让数字保持原样，createdAt 这类毫秒时间戳重新序列化后不变。
func decodePayload(raw []byte) (*payload, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var p payload
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("解析画布内容: %w", err)
	}
	return &p, nil
}

// CollectRefs 找出画布内容里所有会被导出的素材 ID 和角色卡 ID，供调用方在 Build 之前查出素材哈希和角色卡。
//
// 素材 ID 取自 data.assetId、选中那一版产物的 assetId（其余版本不导出，不收集）、
// data.params.images / videos / audios 的每一项、data.paramAssets 的键、data.scene.thumb.assetId；
// 角色卡 ID 取自 data.characterId。角色卡自己引用的素材由调用方查卡时一并补上。
func CollectRefs(raw []byte) (Refs, error) {
	p, err := decodePayload(raw)
	if err != nil {
		return Refs{}, err
	}
	var refs Refs
	seenAsset := map[string]bool{}
	seenChar := map[string]bool{}
	for _, node := range p.Nodes {
		data, _ := node["data"].(map[string]any)
		for _, id := range assetIDRefs(data) {
			if !seenAsset[id] {
				seenAsset[id] = true
				refs.AssetIDs = append(refs.AssetIDs, id)
			}
		}
		if id, ok := data["characterId"].(string); ok && id != "" && !seenChar[id] {
			seenChar[id] = true
			refs.CharacterIDs = append(refs.CharacterIDs, id)
		}
	}
	return refs, nil
}

// assetIDRefs 取出一个节点 data 里会被导出的素材 ID（含重复），顺序与 CollectRefs 的说明一致。
func assetIDRefs(data map[string]any) []string {
	if data == nil {
		return nil
	}
	var ids []string
	ids = appendAssetID(ids, data["assetId"])
	if out := keptOutput(data); out != nil {
		ids = appendAssetID(ids, out["assetId"])
	}
	params, _ := data["params"].(map[string]any)
	for _, key := range mediaParamKeys {
		list, _ := params[key].([]any)
		for _, v := range list {
			ids = appendAssetID(ids, v)
		}
	}
	paramAssets, _ := data["paramAssets"].(map[string]any)
	ids = append(ids, sortedKeys(paramAssets)...)
	scene, _ := data["scene"].(map[string]any)
	thumb, _ := scene["thumb"].(map[string]any)
	return appendAssetID(ids, thumb["assetId"])
}

// mediaParamKeys 是 data.params 里存素材 ID 数组的参数名。
var mediaParamKeys = []string{"images", "videos", "audios"}

// keptOutput 返回导出时要保留的那一版产物：id 等于 activeOutputId 的那项；
// 找不到（选中版缺失或没记 activeOutputId）时保留最后一版，因为 outputs 旧的在前、最后一版最新。
// 没有产物返回 nil。
func keptOutput(data map[string]any) map[string]any {
	outputs, _ := data["outputs"].([]any)
	var last map[string]any
	for _, o := range outputs {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		if active, ok := data["activeOutputId"].(string); ok && m["id"] == active {
			return m
		}
		last = m
	}
	return last
}

// assetIDOf 把一个素材 ID 值规整成字符串：前端存的是十进制字符串，偶尔会是 JSON 数字。
func assetIDOf(v any) (string, bool) {
	switch id := v.(type) {
	case string:
		return id, id != ""
	case json.Number:
		return id.String(), true
	default:
		return "", false
	}
}

// appendAssetID 在 v 是素材 ID 时把它追加到 ids，否则原样返回。
func appendAssetID(ids []string, v any) []string {
	if id, ok := assetIDOf(v); ok {
		return append(ids, id)
	}
	return ids
}

// sortedKeys 返回 m 的键并排序，让结果不随 map 遍历顺序变化。
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
