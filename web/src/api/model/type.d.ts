/** 生成方式：视频 t2v 文生 / i2v 图生 / omni 全能参考，图片 t2i 文生图 / i2i 图生图 */
export type GenerationOp = "t2v" | "i2v" | "omni" | "t2i" | "i2i";

/** 参考素材的种类 */
export type RefKind = "image" | "video" | "audio";

/** 一种参考素材的配置 */
export interface RefSpec {
  /** 是否接收这种素材 */
  on: boolean;
  /** 最多几个 */
  max: number;
  /** 单个最大多少 MB */
  max_mb: number;
}

/** 生成参数的取值类型 */
export type ParamType = "enum" | "number" | "boolean";

/** enum 的可选值：字符串或数字 */
export type ParamOption = string | number;

/** capabilities.params 里的一个生成参数 */
export interface ParamField {
  type: ParamType;
  /** 参数面板里的控件标题 */
  label: string;
  /** true 出现在画布参数面板；false 不出现，按 default 发送 */
  open: boolean;
  /** enum 的可选值 */
  options?: ParamOption[];
  default?: ParamOption | boolean;
  /** number：滑块范围与步长（整数） */
  min?: number;
  max?: number;
  step?: number;
  /** 展示用的单位，如「秒」 */
  unit?: string;
  /** 可作为规格价格的条件维度（第三期使用） */
  spec?: boolean;
  /** 生成数量：一次提交拆成 N 个任务（第三期使用） */
  fanout?: boolean;
}

/** 有序对象：键顺序就是画布参数面板的显示顺序（参数名假定为非整数） */
export type ParamSet = Record<string, ParamField>;

/** 模型能力：由运营在后台手填，画布渲染与下单校验的唯一来源 */
export interface Capabilities {
  /** 生成方式；文本、音频模型没有 */
  ops?: GenerationOp[];
  /** 参考素材（仅视频、图片） */
  refs: Record<RefKind, RefSpec>;
  prompt: { max_length: number };
  params?: ParamSet;
  /** 仅文本、agent */
  context?: { window: number; output: number };
  /** 固定系统提示，仅文本；不会下发给画布 */
  system?: string;
  /** 能不能看图，仅 agent；不能看图的模型拿不到「看图」工具 */
  vision?: boolean;
}

/** 计费方式：按次 / 按秒（× 时长参数 duration）/ 按 Token（仅文本） */
export type Billing = "per_call" | "per_second" | "token";

/** Token 单价（积分 / 百万 Token） */
export interface TokenPrice {
  in: number;
  out: number;
}

/** 一条规格价格：满足 when 的全部条件时覆盖默认价；条件最多的一条胜出 */
export interface PriceTier {
  /** 是否可供用户使用 */
  on: boolean;
  /** 条件：spec 参数名 -> 取值，或 op（生成方式）、ref_video（参考素材里有视频） */
  when: Record<string, string | number | boolean>;
  /** 价格，单位随计费方式（积分 / 次、积分 / 秒） */
  unit: number;
}

/** 积分成本，仅管理端可见 */
export interface PriceCost {
  on: boolean;
  unit?: number;
  per_second?: number;
  token?: TokenPrice;
}

/** 模型定价：价格一律是整数积分 */
export interface Pricing {
  billing: Billing;
  /** 按次：积分 / 次 */
  unit?: number;
  /** 按秒：积分 / 秒 */
  per_second?: number;
  /** 按 Token：输入价与输出价 */
  token?: TokenPrice;
  tiers?: PriceTier[];
  /** 画布接口不下发 */
  cost?: PriceCost;
}

/** GET /models 返回的一项 */
export interface ModelInfo {
  /** 模型 key */
  key: string;
  /** 模型类型（image / video 等） */
  kind: string;
  /** 展示名称 */
  label: string;
  /** 模型简介提示 */
  hint?: string;
  /** 厂商 slug（如 kling），对应内置 logo；空串表示没有，回退首字头像 */
  vendor?: string;
  /** 展示标签，没有时为空数组 */
  tags?: string[];
  /** 定价（不含积分成本），画布据此本地计价 */
  pricing: Pricing;
  /** 模型能力：生成方式、参考素材、提示词上限与生成参数 */
  capabilities: Capabilities;
}
