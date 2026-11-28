package repository

import "encoding/json"

const collaborationManualReferences = "data.manualReferences"

var collaborationReferenceKinds = map[string]string{"images": "image", "videos": "video", "audios": "audio"}

type collaborationManualReference struct {
	AssetID string `json:"assetId"`
	URL     string `json:"url,omitempty"`
}

func (fields *collaborationFields) manualReferences() json.RawMessage {
	if raw := fields.data["manualReferences"]; raw != nil {
		return raw
	}
	params, assets := fields.promptParams(), fields.paramAssets()
	references := make(map[string][]collaborationManualReference)
	for key := range collaborationReferenceKinds {
		var ids []json.RawMessage
		_ = json.Unmarshal(params[key], &ids)
		for _, raw := range ids {
			id := collaborationScalarID(raw)
			if id == "" {
				continue
			}
			url, _ := decodeCollaborationObject(assets[id])["url"].(string)
			references[key] = append(references[key], collaborationManualReference{AssetID: id, URL: url})
		}
	}
	value, _ := json.Marshal(references)
	return value
}

func collaborationScalarID(raw json.RawMessage) string {
	if id := collaborationString(raw); id != "" {
		return id
	}
	var number json.Number
	_ = json.Unmarshal(raw, &number)
	return number.String()
}

func (fields *collaborationFields) setManualReferences(raw json.RawMessage) {
	var previous, next map[string][]collaborationManualReference
	_ = json.Unmarshal(fields.manualReferences(), &previous)
	_ = json.Unmarshal(raw, &next)
	params, assets := fields.promptParams(), fields.paramAssets()
	if assets == nil {
		assets = make(map[string]json.RawMessage)
	}
	for key, kind := range collaborationReferenceKinds {
		for _, ref := range previous[key] {
			delete(assets, ref.AssetID)
		}
		delete(params, key)
		if len(next[key]) == 0 {
			continue
		}
		ids := make([]string, 0, len(next[key]))
		for _, ref := range next[key] {
			ids = append(ids, ref.AssetID)
			if ref.URL != "" {
				assets[ref.AssetID], _ = json.Marshal(map[string]string{"url": ref.URL, "mediaType": kind})
			}
		}
		params[key], _ = json.Marshal(ids)
	}
	delete(fields.data, "manualReferences")
	fields.data["params"], _ = json.Marshal(params)
	fields.data["paramAssets"], _ = json.Marshal(assets)
}
