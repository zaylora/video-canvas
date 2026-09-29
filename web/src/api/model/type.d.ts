/** input_schema 里字段的取值类型 */
export type InputFieldType =
  | 'text'
  | 'number'
  | 'enum'
  | 'boolean'
  | 'image'
  | 'video'
  | 'audio'

/** 可由上游连线提供值的端口类型 */
export type InputPortType = 'text' | 'image' | 'video' | 'audio'

/** enum 字段的一个可选项 */
export interface InputFieldOption {
  /** 提交给后端的值 */
  value: string | number | boolean
  /** 展示给用户的文案 */
  label: string
}

/** input_schema 中的一个字段 */
export interface InputFieldSchema {
  /** 字段取值类型 */
  type: InputFieldType
  /** 展示名称 */
  label: string
  /** 是否必填 */
  required?: boolean
  /** 默认值 */
  default?: unknown
  /** number 字段的最小值 */
  min?: number
  /** number 字段的最大值 */
  max?: number
  /** text 字段的最大长度 */
  max_length?: number
  /** enum 字段的可选项 */
  options?: InputFieldOption[]
  /** 有值表示这个字段可以由上游节点连线提供 */
  port?: InputPortType
  /** 折叠到「高级」里 */
  advanced?: boolean
}

/** 有序对象：键顺序就是渲染顺序（字段名假定为非整数） */
export type InputSchema = Record<string, InputFieldSchema>

/** GET /models 返回的一项 */
export interface ModelInfo {
  /** 模型 key */
  key: string
  /** 模型类型（image / video 等） */
  kind: string
  /** 展示名称 */
  label: string
  /** 模型简介提示 */
  hint?: string
  /** 单次生成消耗的积分 */
  credits: number
  /** 模型输入参数定义 */
  input_schema: InputSchema
}
