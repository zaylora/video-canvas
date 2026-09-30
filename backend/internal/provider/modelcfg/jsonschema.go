// 本文件：生成模型配置自身结构的 JSON Schema（draft-07），给前端 Monaco 编辑器做补全和基础校验。

package modelcfg

import (
	"encoding/json"
	"sync"
)

var (
	schemaOnce  sync.Once
	schemaCache []byte
)

// buildSchema 生成并缓存 JSON Schema 文本。
func buildSchema() {
	b, err := json.MarshalIndent(modelSchema(), "", "  ")
	if err != nil {
		panic("modelcfg: 生成 JSON Schema 失败：" + err.Error())
	}
	schemaCache = b
}

type obj = map[string]any

func str(desc string) obj  { return obj{"type": "string", "description": desc} }
func num(desc string) obj  { return obj{"type": "number", "description": desc} }
func boolean(d string) obj { return obj{"type": "boolean", "description": d} }
func enum(desc string, values ...string) obj {
	return obj{"type": "string", "enum": values, "description": desc}
}
func duration(desc string) obj {
	return obj{
		"type":        []string{"string", "number"},
		"pattern":     `^[0-9]+(\.[0-9]+)?(ms|s|m|h)$`,
		"description": desc + "，写成 \"10s\" / \"30m\" 这样的字符串",
		"examples":    []string{"10s", "30m"},
	}
}
func object(desc string, props obj, required ...string) obj {
	o := obj{"type": "object", "properties": props, "additionalProperties": false, "description": desc}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

// inputFieldSchema 是 input_schema 里单个字段定义的 Schema。
func inputFieldSchema() obj {
	return object("输入字段定义", obj{
		"type":       enum("字段类型", validFieldTypes...),
		"label":      str("控件标题，也用于错误提示"),
		"required":   boolean("是否必填"),
		"default":    obj{"description": "默认值，类型需与字段类型一致（媒体字段不能设置）"},
		"min":        num("number 的最小值"),
		"max":        num("number 的最大值"),
		"max_length": obj{"type": "integer", "minimum": 0, "description": "text 的最大字符数"},
		"options": obj{
			"type": "array", "description": "enum 的选项",
			"items": object("枚举选项", obj{
				"value": obj{"type": []string{"string", "number"}, "description": "选项值"},
				"label": str("显示名"),
			}, "value", "label"),
		},
		"port":     enum("可由画布上游连线提供的端口类型", validPorts...),
		"advanced": boolean("折叠到“高级”里"),
	}, "type", "label")
}

// channelRefSchema 是 channels 数组元素的 Schema。
func channelRefSchema() obj {
	return object("模型绑定的一个渠道", obj{
		"channel": obj{
			"type": "string", "pattern": channelKeyRe.String(),
			"description": "渠道 key（在渠道管理里创建），小写字母、数字、连字符",
		},
		"upstream_model": obj{
			"type": "string", "minLength": 1, "maxLength": maxUpstreamModelLen,
			"description": "这个渠道上的上游模型名（工作流型插件里是工作流 id），由渠道所用的插件解释",
		},
	}, "channel", "upstream_model")
}

func modelSchema() obj {
	return obj{
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"title":                "Model（模型）",
		"type":                 "object",
		"description":          "画布用户选择的一项模型：绑定哪个渠道与上游模型、固定参数、给用户看的输入参数、积分",
		"additionalProperties": false,
		"required":             []string{"key", "kind", "label", "channels"},
		"properties": obj{
			"key":      obj{"type": "string", "pattern": modelKeyRe.String(), "description": "模型唯一标识"},
			"kind":     enum("模型类型", Kinds...),
			"label":    str("下拉里显示的名字"),
			"hint":     str("下拉里的一行小字，可省略"),
			"credits":  obj{"type": "integer", "minimum": 0, "description": "每次生成扣的积分"},
			"deadline": duration("任务整体截止时间，大于 0 且不超过 24h，默认 30m"),
			"enabled":  boolean("是否上架"),
			"sort":     obj{"type": "integer", "description": "排序，升序"},
			"channels": obj{
				"type": "array", "minItems": maxChannels, "maxItems": maxChannels, "items": channelRefSchema(),
				"description": "绑定的渠道。数据结构是数组，预留多渠道故障切换，首期只支持一个渠道",
			},
			"params": obj{"type": "object", "description": "固定参数：宿主只存不解释，原样交给渠道所用的插件（例如工作流型插件的节点绑定）"},
			"input_schema": obj{
				"type":                 "object",
				"additionalProperties": inputFieldSchema(),
				"description":          "输入字段定义：字段名 -> 定义。前端按书写顺序渲染参数面板，后端据此校验用户输入",
			},
		},
	}
}
