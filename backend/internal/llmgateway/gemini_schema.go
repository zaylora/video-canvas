package llmgateway

import (
	"encoding/json"
	"fmt"
)

// CleanSchemaForGemini 清洗工具参数中的 Gemini 不支持的关键字，保留约束的文字说明。
func CleanSchemaForGemini(schema json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(schema, &value) != nil {
		return schema
	}
	cleanGeminiSchema(value)
	body, err := json.Marshal(value)
	if err != nil {
		return schema
	}
	return body
}

func cleanGeminiSchema(value any) {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			cleanGeminiSchema(child)
		}
	case map[string]any:
		cleanGeminiAnyOf(node)
		appendSchemaRange(node, "minItems", "maxItems", true)
		appendSchemaRange(node, "minimum", "maximum", false)
		for _, key := range []string{"patternProperties", "additionalProperties", "$schema", "const", "minItems", "maxItems", "minimum", "maximum"} {
			delete(node, key)
		}
		cleanGeminiSchemaChildren(node)
	}
}

func cleanGeminiAnyOf(node map[string]any) {
	branches, ok := node["anyOf"].([]any)
	if !ok || len(branches) == 0 {
		return
	}
	kind := ""
	mixed, enumerated := false, true
	var values []any
	for i, branch := range branches {
		b, ok := branch.(map[string]any)
		if !ok {
			enumerated = false
			continue
		}
		typ, _ := b["type"].(string)
		if i == 0 {
			kind = typ
		} else if typ != kind {
			mixed = true
		}
		vals, ok := geminiEnumValues(b)
		enumerated = enumerated && ok
		values = append(values, vals...)
	}
	if mixed {
		delete(node, "anyOf")
		delete(node, "enum")
		node["type"] = "string"
		appendSchemaDescription(node, `（也可以写秒数，例如 "3.5"）`)
	} else if enumerated && kind != "" {
		delete(node, "anyOf")
		node["type"] = kind
		node["enum"] = values
	}
}

func geminiEnumValues(branch map[string]any) ([]any, bool) {
	for key := range branch {
		if key != "type" && key != "const" && key != "enum" && key != "description" {
			return nil, false
		}
	}
	if value, ok := branch["const"]; ok {
		return []any{value}, true
	}
	values, ok := branch["enum"].([]any)
	return values, ok
}

func cleanGeminiSchemaChildren(node map[string]any) {
	for key, child := range node {
		switch key {
		case "properties", "$defs", "definitions":
			// 这些对象的键是参数名，不是 schema 关键字，不能把同名参数误删。
			if props, ok := child.(map[string]any); ok {
				for _, prop := range props {
					cleanGeminiSchema(prop)
				}
			}
		case "enum", "default", "examples":
			// 这里是参数取值，不是子 schema。
		default:
			cleanGeminiSchema(child)
		}
	}
}

func appendSchemaDescription(node map[string]any, suffix string) {
	description, _ := node["description"].(string)
	node["description"] = description + suffix
}

func appendSchemaRange(node map[string]any, minKey, maxKey string, items bool) {
	lower, hasMin := node[minKey]
	upper, hasMax := node[maxKey]
	switch {
	case hasMin && hasMax:
		unit := ""
		if items {
			unit = " 项"
		}
		appendSchemaDescription(node, fmt.Sprintf("（%v 到 %v%s）", lower, upper, unit))
	case hasMin:
		appendSchemaDescription(node, fmt.Sprintf("（至少 %v）", lower))
	case hasMax:
		appendSchemaDescription(node, fmt.Sprintf("（最多 %v）", upper))
	}
}
