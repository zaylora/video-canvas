/** 环节输入的媒体需求 */
export type StepMedia = "none" | "images" | "video";

/** 方法来源：内置方法或导入的技能 */
export type StepSource = "builtin" | "skill";

/** 环节当前绑定的方法 */
export interface StepBinding {
  /** 内置或技能 */
  source: StepSource;
  /** 技能名；内置没有 */
  skill_name?: string;
  /** 技能显示名 */
  skill_title?: string;
  /** 钉死的技能版本 */
  version?: number;
}

/** 环节实际生效的方法：技能停用 / 版本删除时退回内置 */
export interface StepEffective {
  /** 内置或技能 */
  source: StepSource;
  /** 技能名 */
  skill_name?: string;
  /** 版本 */
  version?: number;
  /** 绑定的技能不可用，已自动退回内置 */
  fallback?: boolean;
}

/** 最近一次试跑 */
export interface StepLastTrial {
  /** 试跑 ID */
  id: string;
  /** 格式校验是否通过 */
  passed: boolean;
  /** 试跑时间 */
  created_at: string;
}

/** GET /admin/steps 的一行 */
export interface StepSummary {
  /** 环节 key，如 marketing.reverse */
  key: string;
  /** 环节名 */
  title: string;
  /** 所属工作室 */
  studio: string;
  /** 输入媒体 */
  media: StepMedia;
  /** 当前绑定 */
  binding: StepBinding;
  /** 实际生效 */
  effective: StepEffective;
  /** 绑定技能有更新的生效版本时给出，否则 null */
  newer_version: number | null;
  /** 最近一次试跑；没有为 null */
  last_trial: StepLastTrial | null;
  /** 近 7 天失败率，0–1 */
  fail_rate_7d: number;
}

/** 环节的输入变量 */
export interface StepInput {
  /** 变量名 */
  name: string;
  /** 类型 */
  type: string;
  /** 说明 */
  desc: string;
}

/** 试跑样例 */
export interface StepSample {
  /** 样例 key */
  key: string;
  /** 展示名 */
  name: string;
  /** 需要带参考片 */
  needs_ref: boolean;
}

/** 绑定历史 */
export interface StepHistory {
  /** 记录 ID */
  id: string;
  /** 换之前；没有（初始）为 null，后端也可能给展示串 */
  from: StepBinding | string | null;
  /** 换之后，同上 */
  to: StepBinding | string | null;
  /** 操作人 */
  actor: string;
  /** 通过的试跑 ID */
  trial_id: string;
  /** 时间 */
  created_at: string;
}

/** 候选方法：内置，或某个已导入的技能 */
export type StepCandidateInfo =
  | {
      /** 内置方法 */
      source: "builtin";
    }
  | {
      /** 导入的技能 */
      source: "skill";
      /** 技能名 */
      skill_name: string;
      /** 技能显示名 */
      skill_title: string;
      /** 可选版本 */
      versions: number[];
      /** 当前生效版本 */
      active_version: number;
      /** 技能里带脚本（脚本不执行，只用正文） */
      has_scripts: boolean;
      /** 正文字数 */
      chars: number;
      /** 技能已启用 */
      enabled: boolean;
    };

/** GET /admin/steps/:key 的详情 */
export interface StepDetail extends StepSummary {
  /** 本步职责 */
  duty: string;
  /** 输入变量 */
  inputs: StepInput[];
  /** 输出 JSON Schema 文本 */
  schema: string;
  /** 校验规则 */
  rules: { hard: string[]; soft: string[] };
  /** 试跑样例 */
  samples: StepSample[];
  /** 绑定历史，新的在后 */
  history: StepHistory[];
  /** 候选方法 */
  candidates: StepCandidateInfo[];
}

/** 试跑 / 绑定请求里的候选 */
export interface TrialCandidate {
  /** 内置或技能 */
  source: StepSource;
  /** 技能名 */
  skill_name?: string;
  /** 版本 */
  version?: number;
}

/** 试跑状态；failed 表示调用本身出错 */
export type TrialStatus = "queued" | "running" | "succeeded" | "failed";

/** 一次试跑 */
export interface TrialView {
  /** 试跑 ID */
  id: string;
  /** 环节 key */
  step_key: string;
  /** 候选 */
  candidate: TrialCandidate;
  /** 样例 */
  sample_key: string;
  /** 文本模型 */
  text_model: string;
  /** 对比试跑的另一条；候选本身为 null */
  compare_of: string | null;
  /** 状态 */
  status: TrialStatus;
  /** 格式校验是否通过 */
  passed: boolean;
  /** 硬错误 */
  errors: string[] | null;
  /** 软问题 */
  flags: string[] | null;
  /** 人能读的输出摘要 */
  preview: string;
  /** 原始输出 JSON；解析失败为 null */
  output: unknown;
  /** 耗时（毫秒） */
  duration_ms: number;
  /** 输入 token */
  input_tokens: number;
  /** 输出 token */
  output_tokens: number;
  /** 积分 */
  credits: number;
  /** 创建时间 */
  created_at: string;
}

/** 提交试跑的请求 */
export interface StartTrialInput {
  /** 候选 */
  candidate: TrialCandidate;
  /** 样例 key */
  sample_key: string;
  /** 文本模型 key */
  text_model: string;
  /** 同时跑当前绑定做对比 */
  compare: boolean;
}

/** 设为生效的请求 */
export interface BindInput {
  /** 内置或技能 */
  source: StepSource;
  /** 技能名 */
  skill_name?: string;
  /** 版本 */
  version?: number;
  /** 通过的试跑 ID */
  trial_id: string;
}

/** 视频提示词格式化器（代码维护，只读） */
export interface FormatterInfo {
  /** 写法 key */
  key: string;
  /** 名称 */
  name: string;
  /** 说明 */
  description: string;
  /** 样例提示词 */
  sample: string;
}
