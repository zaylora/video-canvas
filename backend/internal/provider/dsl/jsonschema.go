// 本文件：生成 DSL 自身结构的 JSON Schema（draft-07），给前端 Monaco 编辑器做补全和基础校验。

package dsl

import (
	"encoding/json"
	"sync"
)

// JSONSchema 返回 DSL 自身结构的 JSON Schema（draft-07），给前端 Monaco 编辑器做补全和基础校验。
// target 为 "provider" 或 "model"，其他值返回 nil。
//
// 它只覆盖字段、枚举和基本类型；表达式是否合法、引用是否存在由 ParseProvider / ParseModel 负责。
func JSONSchema(target string) []byte {
	schemaOnce.Do(buildSchemas)
	return schemaCache[target]
}

var (
	schemaOnce  sync.Once
	schemaCache map[string][]byte
)

type obj = map[string]any

func buildSchemas() {
	schemaCache = map[string][]byte{}
	for name, s := range map[string]obj{"provider": providerSchema(), "model": modelSchema()} {
		b, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			panic("dsl: 生成 JSON Schema 失败：" + err.Error())
		}
		schemaCache[name] = b
	}
}

func str(desc string) obj  { return obj{"type": "string", "description": desc} }
func num(desc string) obj  { return obj{"type": "number", "description": desc} }
func boolean(d string) obj { return obj{"type": "boolean", "description": d} }
func enum(desc string, values ...string) obj {
	return obj{"type": "string", "enum": values, "description": desc}
}
func strMap(desc string) obj {
	return obj{"type": "object", "additionalProperties": obj{"type": "string"}, "description": desc}
}
func duration(desc string) obj {
	return obj{
		"type":        []string{"string", "number"},
		"pattern":     `^[0-9]+(\.[0-9]+)?(ms|s|m|h)$`,
		"description": desc + "，写成 \"10s\" / \"30m\" 这样的字符串",
		"examples":    []string{"10s", "5m"},
	}
}
func object(desc string, props obj, required ...string) obj {
	o := obj{"type": "object", "properties": props, "additionalProperties": false, "description": desc}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

// exprHint 是所有表达式字段共用的说明。
const exprHint = "表达式（expr-lang 语法），可用函数：parseJSON / matches / coalesce / toString / toInt，支持 ?? 和 ?. "

func templateValue(desc string) obj {
	return obj{"description": desc + "。字符串里可写 ${ 表达式 }：整段是一个表达式时保留类型，嵌在文本里时做字符串插值"}
}

func operationSchema(name string, extractKeys []string) obj {
	extract := obj{}
	for _, k := range extractKeys {
		extract[k] = str(exprHint + "（可用变量：input files model task ctx resp status）")
	}
	extractSchema := obj{"type": "object", "properties": extract, "additionalProperties": false,
		"description": "从响应里提取字段：名字 -> 表达式，可用变量 resp（响应 JSON）与 status（HTTP 状态码）"}
	return object(name+" 操作：一次 HTTP 调用的声明", obj{
		"method":  enum("HTTP 方法", "GET", "POST", "PUT", "PATCH", "DELETE"),
		"path":    str("相对 base_url 的路径，必须以 / 开头，可以写 ${ 表达式 }"),
		"headers": strMap("附加请求头，值可以写 ${ 表达式 }（鉴权头由 auth 自动添加，不要写在这里）"),
		"query":   strMap("查询参数，值可以写 ${ 表达式 }"),
		"encoding": object("请求体编码", obj{
			"type":       enum("json 或 multipart", "json", "multipart"),
			"file_field": str("multipart 时文件所在的表单字段名（上传操作必填）"),
		}),
		"body":    templateValue("请求体模板；json 时是任意 JSON，multipart 时是对象（每个键一个表单字段）"),
		"success": str("判断调用是否成功的表达式，返回 bool，可用变量 status / resp。" + exprHint),
		"extract": extractSchema,
		"timeout": duration("该操作的超时，默认 30s"),
	}, "method", "path")
}

func providerSchema() obj {
	ops := object("平台的四个操作；cancel 省略或为 null 表示平台不支持取消", obj{
		"upload": operationSchema("upload", []string{"ref"}),
		"submit": operationSchema("submit", []string{"provider_task_id", "error_code", "error_message"}),
		"query":  operationSchema("query", []string{"status", "outputs", "error_code", "error_message", "provider_cost", "progress"}),
		"cancel": operationSchema("cancel", nil),
	}, "submit", "query")
	// cancel 允许写 null
	ops["properties"].(obj)["cancel"] = obj{"oneOf": []obj{{"type": "null"}, operationSchema("cancel", nil)}, "description": "取消操作；null 表示平台不支持取消"}
	ops["properties"].(obj)["upload"] = obj{"oneOf": []obj{{"type": "null"}, operationSchema("upload", []string{"ref"})}, "description": "上传素材操作；省略表示直接给平台自有存储的签名 URL"}

	return obj{
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"title":                "Provider（平台协议）",
		"type":                 "object",
		"description":          "生成平台的协议配置：鉴权、提交、查询、上传、取消、状态映射、错误分类",
		"additionalProperties": false,
		"required":             []string{"dsl", "key", "name", "base_url", "allowed_hosts", "auth", "operations", "status_map"},
		"properties": obj{
			"dsl":      obj{"const": Version, "description": "DSL 版本号，当前只能是 1"},
			"key":      obj{"type": "string", "pattern": "^[a-z0-9][a-z0-9_-]{0,63}$", "description": "平台唯一标识，小写字母、数字、下划线、连字符"},
			"name":     str("平台显示名"),
			"base_url": str("接口根地址，例如 https://www.runninghub.cn，域名必须在 allowed_hosts 内"),
			"allowed_hosts": obj{
				"type": "array", "minItems": 1, "items": obj{"type": "string"},
				"description": "允许访问的域名白名单，支持 *.example.com 通配；请求、重定向、结果下载都会校验",
			},
			"auth": object("鉴权方式（凭证只能通过 secret 引用名字，明文永远不进配置）", obj{
				"type":   enum("鉴权类型", AuthNone, AuthBearer, AuthHeader, AuthQuery, AuthBodyField),
				"secret": str("凭证名字（在凭证管理里设置），不是明文"),
				"name":   str("header 的头名 / query 的参数名 / body_field 的字段名"),
			}, "type"),
			"rate_limit": object("每个平台一个令牌桶 + 并发上限，0 表示不限制", obj{
				"rps":             num("每秒请求数上限"),
				"max_concurrency": obj{"type": "integer", "minimum": 0, "description": "最大并发请求数"},
			}),
			"poll": object("轮询节奏", obj{
				"first_delay":  duration("提交后第一次轮询前的等待"),
				"interval":     duration("轮询间隔"),
				"max_interval": duration("退避后的最大轮询间隔"),
				"jitter":       obj{"type": "number", "minimum": 0, "maximum": 1, "description": "抖动比例 0–1"},
			}),
			"operations": ops,
			"status_map": obj{
				"type":                 "object",
				"additionalProperties": obj{"type": "string", "enum": []string{"queued", "running", "succeeded", "failed"}},
				"description":          "平台状态 -> 统一状态（queued / running / succeeded / failed）；\"_default\" 是找不到时的兜底",
			},
			"error_rules": obj{
				"type": "array",
				"items": object("按顺序匹配，第一条 when 为真的生效", obj{
					"when":  str("表达式，可用变量 status / resp，例如 status == 429 || status >= 500"),
					"class": enum("错误分类", "retryable", "terminal", "moderation", "provider_balance"),
				}, "when", "class"),
				"description": "错误分类规则；都不匹配时 429 / 5xx 按 retryable，其余按 terminal",
			},
			"webhook": object("回调：只用来触发立即轮询，不信任回调内容", obj{
				"verify":  object("回调校验方式", obj{"type": enum("目前只支持 path_secret", "path_secret")}, "type"),
				"task_id": str("从回调体 req 里取平台任务 id 的表达式"),
			}, "verify", "task_id"),
		},
	}
}

func modelSchema() obj {
	fieldTypes := []string{FieldText, FieldNumber, FieldEnum, FieldBoolean, FieldImage, FieldVideo, FieldAudio}
	field := object("输入字段定义", obj{
		"type":       enum("字段类型", fieldTypes...),
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
		"port":     enum("可由画布上游连线提供的端口类型", "text", "image", "video", "audio"),
		"advanced": boolean("折叠到“高级”里"),
	}, "type", "label")

	return obj{
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"title":                "Model（模型 / 工作流）",
		"type":                 "object",
		"description":          "画布用户选择的一项模型：输入参数、到平台请求的映射、输出选择、积分",
		"additionalProperties": false,
		"required":             []string{"key", "kind", "provider", "label", "input_schema"},
		"properties": obj{
			"key":      obj{"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$", "description": "模型唯一标识"},
			"kind":     enum("模型类型", "video", "image", "audio"),
			"provider": str("所属平台的 key"),
			"label":    str("下拉里显示的名字"),
			"hint":     str("下拉里的一行小字"),
			"credits":  obj{"type": "integer", "minimum": 0, "description": "每次生成扣的积分"},
			"deadline": duration("任务整体截止时间，默认 30m"),
			"enabled":  boolean("是否上架"),
			"sort":     obj{"type": "integer", "description": "排序，升序"},
			"params":   obj{"type": "object", "description": "给 Provider 模板用的参数，在表达式里通过 model.params.xxx 访问，例如 webappId"},
			"input_schema": obj{
				"type":                 "object",
				"additionalProperties": field,
				"description":          "输入字段定义：字段名 -> 定义。前端按书写顺序渲染参数面板，后端据此校验用户输入",
			},
			"mapping": templateValue("输入 -> 平台字段的映射模板，渲染后在 Provider 模板里通过 model.mapping 访问。可用变量：input / files / model / ctx"),
			"output": object("从平台产物里选出要转存的那些", obj{
				"select": str("表达式，输入 outputs（每项有 url / type / node / text），返回数组。" + exprHint + "省略时取全部产物"),
				"media":  enum("产物的媒体类型，省略时取模型 kind", "video", "image", "audio"),
			}),
		},
	}
}
