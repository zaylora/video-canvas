import type { AgentEventDto, CanvasPatchDto } from "@/api/agent/type";
/** 生成任务状态 */
export type TaskStatus =
  /** 已创建，待入队 */
  | "pending"
  /** 已入队等待执行 */
  | "queued"
  /** 执行中 */
  | "running"
  /** 上游已完成，正在落库素材 */
  | "finalizing"
  /** 成功 */
  | "succeeded"
  /** 失败 */
  | "failed"
  /** 已取消 */
  | "canceled"
  /** 已超时过期 */
  | "expired";

/** 任务产出的一份素材 */
export interface TaskOutput {
  /** 素材 ID；文本产出没有 */
  asset_id?: number | string;
  /** 素材访问地址；文本产出没有 */
  url?: string;
  /** 文本正文；仅 media_type 为 text 的产出有 */
  text?: string;
  /** 媒体类型（image / video / audio / text） */
  media_type: string;
  /** 时长（毫秒） */
  duration_ms?: number | null;
  /** 宽度（像素） */
  width?: number | null;
  /** 高度（像素） */
  height?: number | null;
}

/** 后端任务快照，version 递增，只认最大的那份 */
export interface TaskView {
  /** 任务 ID */
  id: number | string;
  /** 所属画布 ID，试跑任务等没有画布时为 null */
  canvas_id: string | null;
  /** 所属画布节点 ID */
  node_id: string;
  /** 任务类型（image / video / text 等） */
  kind: string;
  /** 使用的模型 key */
  model_id: string;
  /** 任务状态 */
  status: TaskStatus;
  /** 进度，未知为 null */
  progress: number | null;
  /** 产出素材列表，未完成为 null */
  outputs: TaskOutput[] | null;
  /** 失败错误码 */
  error_code: string | null;
  /** 失败原因 */
  error_message: string | null;
  /** 任务编号：任务 ID 的十六进制编码，失败时展示给用户，后端日志里的 task_id 就是它，用来定位问题 */
  task_ref?: string;
  /** 冻结的积分 */
  credits: number;
  /** 实际扣的积分（Token 计费按用量结算，可能小于冻结额），未结算为 null */
  charged_credits: number | null;
  /** 快照版本号，递增 */
  version: number;
  /** 任务截止时间 */
  deadline_at: string | null;
  /** 创建时间 */
  created_at: string;
  /** 提交给平台（开始调用上游）的时间，还在排队时为 null */
  submitted_at: string | null;
  /** 结束时间，未结束为 null */
  finished_at: string | null;
}

/** 提交生成任务的请求体 */
export interface CreateTaskRequest {
  /** 任务类型 */
  kind: string;
  /** 使用的模型 key */
  model_id: string;
  /** 所属画布 ID，十六进制串 */
  canvas_id: string;
  /** 所属画布节点 ID */
  node_id: string;
  /** 每个任务绑定的节点，长度等于生成数量；第 i 个任务绑定第 i 个节点 */
  node_ids?: string[];
  /** 模型输入参数，字段由模型 capabilities 决定（prompt、op、生成参数，以及 images / videos / audios 素材 id 数组） */
  input: Record<string, unknown>;
}

/** 提交结果里一个节点的错误，含义与整体请求的 HTTP 错误一致（402 积分不足、429 并发已满……） */
export interface CreateTaskItemError {
  status: number;
  code: number;
  message: string;
}

/** 提交结果的一项：这个节点的任务，或这个节点的错误 */
export interface CreateTaskItem {
  node_id: string;
  task?: TaskView;
  error?: CreateTaskItemError;
}

/** 提交生成任务的结果：按节点顺序逐项给出 */
export interface CreateTaskResponse {
  items: CreateTaskItem[];
}

/** WebSocket 服务端消息 */
export type ServerMessage =
  /** 连接建立后的问候 */
  | { type: "hello"; data?: { server_time?: string } }
  /** 任务状态更新推送 */
  | { type: "task.updated"; channel?: string; data: TaskView }
  /** Agent 改了画布：带逐节点的改动，前端并进当前画布 */
  | { type: "canvas.patch"; channel?: string; data: CanvasPatchDto }
  /** 画布 Agent 会话里的一条事件（文本增量、工具调用、审批……） */
  | { type: "agent.event"; channel?: string; data: AgentEventDto }
  /** 心跳应答 */
  | { type: "pong" }
  /** 服务端错误 */
  | { type: "error"; data?: { code?: number; msg?: string } };
