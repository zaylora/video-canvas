import type { TaskView } from "@/api/generation-task/type";

/** 首页直接生成支持的种类：也是输入卡片的创作模式 */
export type GenerateKind = "image" | "video" | "audio";

/** 一段对话（侧栏里的一项） */
export interface ConversationDto {
  /** 对话 ID，十六进制串 */
  id: string;
  /** 标题 */
  title: string;
  /** 是否默认创作：置顶，不能删除 */
  isDefault: boolean;
  /** 记录条数 */
  recordCount: number;
  /** 最近一条记录的时间，没有记录为 null */
  lastRecordAt: string | null;
  /** 是否有进行中的生成任务 */
  active: boolean;
  /** 创建时间 */
  createdAt: string;
}

/** 记录里某一格提交失败的原因 */
export interface RecordSubmitErrorDto {
  /** 第几格，从 0 开始 */
  index: number;
  /** HTTP 状态（402 积分不足、429 并发已满……） */
  status: number;
  /** 业务错误码 */
  code: number;
  /** 给用户看的文案 */
  message: string;
}

/** 对话里的一条生成记录：一次提交对应一条，保存完整输入快照 */
export interface RecordDto {
  /** 记录 ID，十六进制串 */
  id: string;
  /** 所属对话 ID */
  conversationId: string;
  /** 生成种类 */
  kind: GenerateKind;
  /** 模型 key */
  modelId: string;
  /** 提示词 */
  prompt: string;
  /** 提交时的完整输入快照，重新编辑和再次生成从这里复原 */
  input: Record<string, unknown>;
  /** 生成数量，也是结果格子数 */
  count: number;
  /** 与格子一一对应的任务快照；提交失败的格子是 null */
  tasks: Array<TaskView | null>;
  /** 提交失败的格子和原因 */
  submitErrors: RecordSubmitErrorDto[];
  /** 提交时冻结的积分合计 */
  quoteCredits: number;
  /** 创建时间 */
  createdAt: string;
}

/** 一页记录，按新到旧排列 */
export interface RecordPageDto {
  /** 本页记录 */
  items: RecordDto[];
  /** 下一页的游标；没有更多时为 null */
  next: string | null;
}

/** 提交到哪里：默认创作、新建一段对话，或某段对话的 ID */
export type SubmitTarget = "default" | "new" | (string & {});

/** 提交一条生成记录的请求 */
export interface SubmitRecordRequest {
  /** 生成种类 */
  kind: GenerateKind;
  /** 模型 key */
  modelId: string;
  /** 提示词，只有参考图时可以为空 */
  prompt: string;
  /** 完整的生成输入，由 buildTaskInput 组装 */
  input: Record<string, unknown>;
  /** 生成数量，必须等于输入里的数量参数 */
  count: number;
  /** 仅新建对话时作标题；不传取提示词前 16 个字 */
  title?: string;
}

/** 提交结果 */
export interface SubmitRecordResultDto {
  /** 记录所在的对话 ID（default / new 时是解析出的真实对话） */
  conversationId: string;
  /** 新建的记录 */
  record: RecordDto;
}
