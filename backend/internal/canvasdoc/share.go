package canvasdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ProjectShared removes account-specific state and replaces private file URLs with scoped media URLs.
func ProjectShared(raw []byte, mediaURL func(string) string) ([]byte, error) {
	var payload map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil || payload == nil {
		return nil, fmt.Errorf("invalid shared canvas payload: %w", ErrInvalid)
	}
	if int64(len(raw)) > DefaultLimits().MaxBytes {
		return nil, ErrInvalid
	}
	keepFields(payload, "nodes edges viewport")
	if viewport, ok := payload["viewport"].(map[string]any); ok {
		keepFields(viewport, "x y zoom")
	}
	nodes, ok := payload["nodes"].([]any)
	if (!ok && payload["nodes"] != nil) || len(nodes) > DefaultLimits().MaxNodes {
		return nil, ErrInvalid
	}
	for _, value := range nodes {
		if err := projectNode(value, mediaURL); err != nil {
			return nil, err
		}
	}
	edges, _ := payload["edges"].([]any)
	for _, value := range edges {
		edge, _ := value.(map[string]any)
		keepFields(edge, "id source target sourceHandle targetHandle")
	}
	return json.Marshal(payload)
}

func keepFields(obj map[string]any, fields string) {
	allowed := map[string]bool{}
	for _, field := range strings.Fields(fields) {
		allowed[field] = true
	}
	for key := range obj {
		if !allowed[key] {
			delete(obj, key)
		}
	}
}

func cleanSharedValue(value any, mediaURL func(string) string) error {
	switch obj := value.(type) {
	case []any:
		for _, child := range obj {
			if err := cleanSharedValue(child, mediaURL); err != nil {
				return err
			}
		}
	case map[string]any:
		return cleanSharedObject(obj, mediaURL)
	}
	return nil
}

func projectNode(value any, mediaURL func(string) string) error {
	node, _ := value.(map[string]any)
	if node == nil || (node["type"] != "canvas" && node["type"] != "group") {
		return ErrInvalid
	}
	keepFields(node, "id type position origin parentId width height data")
	if pos, ok := node["position"].(map[string]any); ok {
		keepFields(pos, "x y")
	}
	data, _ := node["data"].(map[string]any)
	if kind, _ := data["kind"].(string); kind != "" && !slices.Contains([]string{"script", "image", "video", "audio", "director", "character", "storyboard"}, kind) {
		return ErrInvalid
	}
	if data == nil {
		return ErrInvalid
	}
	normalizeSharedCharacter(data)
	if err := checkSharedKeys(data); err != nil {
		return err
	}
	if err := checkParamReferences(data); err != nil {
		return err
	}
	keepFields(data, "mediaAspect frameAspect outpaintFrom kind label color labelColor prompt text model status assetId src mediaType uploaded fileName params paramAssets character storyboard scene outputs activeOutputId")
	if data["status"] == "running" {
		data["status"] = "idle"
	}
	if err := cleanSharedValue(data, mediaURL); err != nil {
		return err
	}
	return nil
}

func normalizeSharedCharacter(data map[string]any) {
	if data["kind"] != "character" || data["character"] != nil {
		return
	}
	_, hasID := data["characterId"]
	_, hasCache := data["labelCache"]
	if !hasID && !hasCache {
		return
	}
	cache, _ := data["labelCache"].(map[string]any)
	cachedName, _ := cache["name"].(string)
	label, _ := data["label"].(string)
	name := strings.TrimSpace(cachedName)
	if name == "" {
		name = strings.TrimSpace(label)
	}
	if name == "" {
		name = "未命名角色"
	}
	data["character"] = map[string]any{
		"name": name, "look": "基础形象", "description": "", "extras": []any{}, "voice": nil,
	}
}

func cleanSharedObject(obj map[string]any, mediaURL func(string) string) error {
	id := scalarString(obj["assetId"])
	for key, child := range obj {
		switch {
		case key == "activeOutputId" || key == "assetId":
			if !validSharedID(scalarString(child)) {
				return ErrInvalid
			}
		case key == "src" || key == "url":
			if id != "" {
				obj[key] = mediaURL(id)
			} else if child != nil && child != "" {
				return fmt.Errorf("%w: 媒体缺少可交付的素材编号", ErrInvalid)
			}
		case key == "paramAssets":
			if err := cleanParamAssets(child, obj["params"], mediaURL); err != nil {
				return err
			}
		case foreignKeys[key] || key == "chatSessions" || key == "error" || key == "voiceAfterImage":
			delete(obj, key)
		default:
			if err := cleanSharedValue(child, mediaURL); err != nil {
				return err
			}
		}
	}
	return nil
}

func scalarString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case json.Number:
		return item.String()
	default:
		return ""
	}
}

func validSharedID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 64)
	return err == nil && value > 0 && strconv.FormatUint(value, 10) == id
}
func cleanParamAssets(child, parameters any, mediaURL func(string) string) error {
	entries, ok := child.(map[string]any)
	if !ok {
		return ErrInvalid
	}
	for asset, meta := range entries {
		item, ok := meta.(map[string]any)
		id := asset
		if !validSharedID(id) {
			params, _ := parameters.(map[string]any)
			id = scalarString(params[asset])
		}
		if !ok || !validSharedID(id) {
			return ErrInvalid
		}
		keepFields(item, "fileName mediaType url")
		item["url"] = mediaURL(id)
	}
	return nil
}

var sharedFilePath = regexp.MustCompile(`/files/[^[:space:]]`)

func checkSharedKeys(value any) error {
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			if err := checkSharedKeys(child); err != nil {
				return err
			}
		}
	case map[string]any:
		return checkSharedMap(item)
	case string:
		if sharedFilePath.MatchString(item) || strings.HasPrefix(item, "asset://") || strings.HasPrefix(item, "blob:") || strings.HasPrefix(item, "data:") {
			return ErrInvalid
		}
	}
	return nil
}

func checkSharedMap(item map[string]any) error {
	for key, child := range item {
		if key == "src" || key == "url" || foreignKeys[key] || key == "voiceAfterImage" || key == "chatSessions" {
			continue
		}
		name := strings.ToLower(key)
		if strings.HasSuffix(name, "url") || strings.Contains(name, "assetid") || strings.Contains(name, "storagekey") || strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "virtualhuman") {
			return ErrInvalid
		}
		if err := checkSharedKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func checkParamReferences(data map[string]any) error {
	params, _ := data["params"].(map[string]any)
	for _, key := range []string{"images", "videos", "audios"} {
		value := params[key]
		if value == nil {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			return ErrInvalid
		}
		for _, item := range items {
			if !validSharedID(scalarString(item)) {
				return ErrInvalid
			}
		}
	}
	return nil
}
