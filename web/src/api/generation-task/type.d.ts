/** 生成任务状态 */
export type TaskStatus =
  /** 已创建，待入队 */
  | 'pending'
  /** 已入队等待执行 */
  | 'queued'
  /** 执行中 */
  | 'running'
  /** 上游已完成，正在落库素材 */
  | 'finalizing'
  /** 成功 */
  | 'succeeded'
  /** 失败 */
  | 'failed'
  /** 已取消 */
  | 'canceled'
  /** 已超时过期 */
  | 'expired'

/** 任务产出的一份素材 */
export interface TaskOutput {
  /** 素材 ID；文本产出没有 */
  asset_id?: number | string
  /** 素材访问地址；文本产出没有 */
  url?: string
  /** 文本正文；仅 media_type 为 text 的产出有 */
  text?: string
  /** 媒体类型（image / video / audio / text） */
  media_type: string
  /** 时长（毫秒） */
  duration_ms?: number | null
  /** 宽度（像素） */
  width?: number | null
  /** 高度（像素） */
  height?: number | null
}

/** 后端任务快照，version 递增，只认最大的那份 */
export interface TaskView {
  /** 任务 ID */
  id: number | string
  /** 所属画布 ID，试跑任务等没有画布时为 null */
  canvas_id: number | string | null
  /** 所属画布节点 ID */
  node_id: string
  /** 任务类型（image / video / text 等） */
  kind: string
  /** 使用的模型 key */
  model_id: string
  /** 任务状态 */
  status: TaskStatus
  /** 进度，未知为 null */
  progress: number | null
  /** 产出素材列表，未完成为 null */
  outputs: TaskOutput[] | null
  /** 失败错误码 */
  error_code: string | null
  /** 失败原因 */
  error_message: string | null
  /** 消耗（冻结）的积分 */
  credits: number
  /** 快照版本号，递增 */
  version: number
  /** 任务截止时间 */
  deadline_at: string | null
  /** 创建时间 */
  created_at: string
  /** 结束时间，未结束为 null */
  finished_at: string | null
}

/** 提交生成任务的请求体 */
export interface CreateTaskRequest {
  /** 任务类型 */
  kind: string
  /** 使用的模型 key */
  model_id: string
  /** 所属画布 ID */
  canvas_id: number | string
  /** 所属画布节点 ID */
  node_id: string
  /** 模型输入参数，字段由模型 input_schema 决定 */
  input: Record<string, unknown>
}

/** WebSocket 服务端消息 */
export type ServerMessage =
  /** 连接建立后的问候 */
  | { type: 'hello'; data?: { server_time?: string } }
  /** 任务状态更新推送 */
  | { type: 'task.updated'; channel?: string; data: TaskView }
  /** 心跳应答 */
  | { type: 'pong' }
  /** 服务端错误 */
  | { type: 'error'; data?: { code?: number; msg?: string } }
