/** 生成任务队列里各状态的任务数：一个任务从提交、查询到转存全程是同一个 job */
export interface QueueCounts {
  /** 排队中：已入队、等待被执行（含等名额、等下一次轮询） */
  waiting: number;
  /** 运行中 */
  running: number;
  /** 重试中：失败后等待重试 */
  retryable: number;
  /** 近 1 小时被丢弃（重试用尽）的任务数 */
  discarded_1h: number;
}

/** 生成任务队列概况 */
export interface QueueInfo {
  /** 是否已暂停（持久化，对所有实例生效） */
  paused: boolean;
  /** 当前并发（每实例）：一个生成任务占一个名额 */
  concurrency: number;
  /** 并发硬上限（每实例，来自配置文件），并发不能超过它 */
  hard_cap: number;
  /** 各状态任务数 */
  counts: QueueCounts;
}

/** 渠道占用：名额 = 提交中 + 上游生成中 */
export interface QueueChannel {
  /** 渠道 key */
  key: string;
  /** 渠道名称 */
  name: string;
  /** 已占用的名额 */
  occupied: number;
  /** 最大同时生成数；0 表示不限 */
  max_running: number;
  /** 等待名额的任务数 */
  waiting: number;
  /** 最早一个等待名额的任务已等了多少秒；没有等待时为 0 */
  oldest_wait_sec: number;
}

/** 一个实例的负载 */
export interface InstanceQueueStat {
  /** 处理中的任务数 */
  inflight: number;
  /** 该实例当前生效的并发 */
  limit: number;
}

/** 后台工作实例 */
export interface QueueInstance {
  /** 实例 ID */
  id: string;
  /** 主机名 */
  hostname: string;
  /** 应用版本 */
  version: string;
  /** 启动时间（ISO 字符串） */
  started_at: string;
  /** 最近一次心跳（ISO 字符串） */
  heartbeat_at: string;
  /** 是否在线：心跳超过 30 秒判定失联 */
  online: boolean;
  /** 是否是当前的 leader */
  is_leader: boolean;
  /** 处理中任务数与生效并发；实例还没上报时是空对象 */
  stats: Partial<InstanceQueueStat>;
}

/** GET /admin/queue/overview */
export interface QueueOverview {
  /** 生成任务队列 */
  queue: QueueInfo;
  /** 渠道占用 */
  channels: QueueChannel[];
  /** 工作实例 */
  instances: QueueInstance[];
}

/** 按分钟的吞吐点 */
export interface ThroughputPoint {
  /** 整分钟的时间（ISO 字符串） */
  minute: string;
  /** 这一分钟完成（成功）的任务数 */
  succeeded: number;
  /** 这一分钟失败的任务数 */
  failed: number;
  /** 这一分钟内任务的平均排队时长（毫秒）；没有任务时为 0 */
  avg_wait_ms: number;
}

/** PUT /admin/queue/concurrency 的返回 */
export interface SaveConcurrencyResult {
  /** 保存后的并发 */
  concurrency: number;
  /** 已生效的实例数 */
  applied_instances: number;
  /** 在线实例总数 */
  total_instances: number;
}

/** POST /admin/queue/pause|resume 的返回 */
export interface PauseResult {
  /** 操作后是否处于暂停 */
  paused: boolean;
}

/** POST /admin/queue/river-ticket 的返回 */
export interface RiverTicket {
  /** River UI 的同源相对地址（已带一次性 ticket），直接在新标签页打开 */
  url: string;
}
