import type {
  InstanceQueueStat,
  QueueChannel,
  QueueCounts,
  QueueInstance,
  QueueName,
  QueueOverview,
  ThroughputPoint,
} from "@/api/admin/queue/type";

/** admin 看「任务队列」页时，并发 / 暂停 / 恢复控件被禁用的原因 */
export const QUEUE_WRITE_DENY_REASON = "仅运维（super_admin）可修改并发、暂停或恢复队列";

/** admin 看「任务队列」页时，「打开 River UI」被禁用的原因 */
export const RIVER_UI_DENY_REASON = "仅运维（super_admin）可打开 River UI：它能重试、删除任务";

/**
 * 并发与暂停控件的禁用原因
 * @param canWrite 当前角色是否可写（super_admin）
 * @returns 不可写时的原因；可写为 null
 */
export const queueWriteDenyReason = (canWrite: boolean) =>
  canWrite ? null : QUEUE_WRITE_DENY_REASON;

/**
 * 「打开 River UI」的禁用原因
 * @param canWrite 当前角色是否可写（super_admin）
 * @returns 不可写时的原因；可写为 null
 */
export const riverUiDenyReason = (canWrite: boolean) => (canWrite ? null : RIVER_UI_DENY_REASON);

/**
 * 校验并发取值：必须是 1 到硬上限的整数（后端同样会校验，越界返回 400）
 * @param value 要保存的并发
 * @param hardCap 该队列的并发硬上限（每实例）
 * @returns 不合法时的原因；合法为 null
 */
export function validateConcurrency(value: number, hardCap: number): string | null {
  if (!Number.isInteger(value)) return "并发必须是整数";
  if (value < 1 || value > hardCap) return `并发范围 1 到 ${hardCap}`;
  return null;
}

/**
 * 保存草稿是否有改动：草稿还没动过（undefined）或和服务端值相同都不算
 * @param draft 滑杆的草稿值
 * @param saved 服务端当前并发
 */
export const isDraftDirty = (draft: number | undefined, saved: number) =>
  draft !== undefined && draft !== saved;

/**
 * 滑杆应该显示的值：有草稿显示草稿，否则跟着服务端走。
 * 草稿和服务端值分开保存，定时刷新带来的新服务端值不会覆盖正在拖动的草稿
 * @param draft 滑杆的草稿值
 * @param saved 服务端当前并发
 */
export const shownConcurrency = (draft: number | undefined, saved: number) => draft ?? saved;

/** 各队列的草稿并发；没有草稿的队列不出现 */
export type ConcurrencyDrafts = Partial<Record<QueueName, number>>;

/**
 * 写入一个队列的草稿：拖回服务端值就等于没有草稿，这样“还原”按钮和保存按钮自然消失
 * @param drafts 现有草稿
 * @param name 队列名称
 * @param value 新的草稿值
 * @param saved 服务端当前并发
 * @returns 新的草稿对象，不修改入参
 */
export function setDraft(
  drafts: ConcurrencyDrafts,
  name: QueueName,
  value: number,
  saved: number,
): ConcurrencyDrafts {
  const next = { ...drafts };
  if (value === saved) delete next[name];
  else next[name] = value;
  return next;
}

/**
 * 丢弃一个队列的草稿（还原、保存成功、保存失败后回到服务端值）
 * @param drafts 现有草稿
 * @param name 队列名称
 * @returns 新的草稿对象，不修改入参
 */
export function dropDraft(drafts: ConcurrencyDrafts, name: QueueName): ConcurrencyDrafts {
  const next = { ...drafts };
  delete next[name];
  return next;
}

/**
 * 保存成功后的“已生效”说明
 * @param applied 已生效的实例数
 * @param total 在线实例总数
 */
export const appliedText = (applied: number, total: number) =>
  `已生效于 ${applied}/${total} 个实例`;

/**
 * 暂停队列前的确认说明：排队时间照常累计，是暂停的主要代价
 */
export const PAUSE_CONFIRM_TEXT =
  "已入队任务保留，但排队时间继续累计，超过排队超时的任务会过期并退款。";

/**
 * 保存失败后给卡片的就地提示
 * @param reason 失败原因
 * @param restored 已还原到的服务端并发
 */
export const saveFailedText = (reason: string, restored: number) =>
  `保存失败：${reason}，已还原为 ${restored}`;

/**
 * 一个队列里还没做完的任务数：排队 + 延迟 + 运行 + 重试（丢弃的不算）
 * @param counts 各状态计数
 */
export const pendingTotal = (counts: QueueCounts) =>
  counts.available + counts.scheduled + counts.running + counts.retryable;

/**
 * 整个总览是否“空闲”：三个队列都没有未完成任务，也没有渠道在占用或等待名额
 * @param overview 总览
 */
export function isQueueIdle(overview: QueueOverview): boolean {
  return (
    overview.queues.every((queue) => pendingTotal(queue.counts) === 0) &&
    overview.channels.every((channel) => channel.occupied === 0 && channel.waiting === 0)
  );
}

/**
 * 实例是否失联：以后端按心跳判定的 online 为准（心跳超过 30 秒）
 * @param instance 实例
 */
export const isInstanceLost = (instance: Pick<QueueInstance, "online">) => !instance.online;

/**
 * 失联的实例
 * @param instances 全部实例
 */
export const lostInstances = (instances: QueueInstance[]) => instances.filter(isInstanceLost);

/** 吞吐摘要 */
export type ThroughputSummary = {
  /** 完成数合计 */
  succeeded: number;
  /** 失败数合计 */
  failed: number;
  /** 失败率（0 到 100）；一个任务都没有时为 0 */
  failureRate: number;
  /** 平均排队时长（毫秒），按每分钟的任务数加权；没有任务时为 0 */
  avgWaitMs: number;
  /** 是否一个任务都没有 */
  empty: boolean;
};

/**
 * 失败率
 * @param succeeded 完成数
 * @param failed 失败数
 * @returns 0 到 100 的百分数；没有任务时为 0
 */
export function failureRate(succeeded: number, failed: number): number {
  const total = succeeded + failed;
  return total === 0 ? 0 : (failed / total) * 100;
}

/**
 * 百分数的显示：保留一位小数
 * @param value 0 到 100 的百分数
 */
export const formatPercent = (value: number) => `${value.toFixed(1)}%`;

/**
 * 汇总一段时间的吞吐：完成数、失败数、失败率、平均排队时长
 * @param points 按分钟的数据
 */
export function summarizeThroughput(points: ThroughputPoint[]): ThroughputSummary {
  let succeeded = 0;
  let failed = 0;
  let waitWeighted = 0;
  for (const point of points) {
    const count = point.succeeded + point.failed;
    succeeded += point.succeeded;
    failed += point.failed;
    waitWeighted += point.avg_wait_ms * count;
  }
  const total = succeeded + failed;
  return {
    succeeded,
    failed,
    failureRate: failureRate(succeeded, failed),
    avgWaitMs: total === 0 ? 0 : waitWeighted / total,
    empty: total === 0,
  };
}

/** 柱状图的一根柱子：两段高度都是占最高柱的比例（0 到 1） */
export type ThroughputBar = {
  /** 整分钟时间（ISO 字符串） */
  minute: string;
  /** 完成数 */
  succeeded: number;
  /** 失败数 */
  failed: number;
  /** 完成段占最高柱的比例 */
  succeededRatio: number;
  /** 失败段占最高柱的比例 */
  failedRatio: number;
};

/**
 * 把吞吐数据换算成柱子高度：以单分钟总数最高的那根为 1
 * @param points 按分钟的数据
 */
export function throughputBars(points: ThroughputPoint[]): ThroughputBar[] {
  const max = Math.max(0, ...points.map((point) => point.succeeded + point.failed));
  return points.map((point) => ({
    minute: point.minute,
    succeeded: point.succeeded,
    failed: point.failed,
    succeededRatio: max === 0 ? 0 : point.succeeded / max,
    failedRatio: max === 0 ? 0 : point.failed / max,
  }));
}

/**
 * 秒数显示成“6 分 12 秒”这样的中文时长
 * @param seconds 秒；负数按 0
 */
export function formatDuration(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  if (total < 60) return `${total} 秒`;
  const minutes = Math.floor(total / 60);
  const rest = total % 60;
  if (minutes < 60) return rest === 0 ? `${minutes} 分` : `${minutes} 分 ${rest} 秒`;
  const hours = Math.floor(minutes / 60);
  const restMinutes = minutes % 60;
  return restMinutes === 0 ? `${hours} 小时` : `${hours} 小时 ${restMinutes} 分`;
}

/** 渠道占用表一行的显示结果 */
export type ChannelRow = {
  /** 渠道 key，仅作列表 key */
  key: string;
  /** 渠道名称 */
  name: string;
  /** “占用 / 最大同时生成数”，上限为 0 显示“不限” */
  occupancy: string;
  /** 等待名额的任务数 */
  waiting: number;
  /** 最早等待时长；没有等待时为 null */
  oldestWait: string | null;
  /** 是否满载：有上限且占用已达上限 */
  full: boolean;
};

/**
 * 渠道占用行：max_running 为 0 表示不限，永远不算满载
 * @param channel 后端的渠道占用
 */
export function channelRow(channel: QueueChannel): ChannelRow {
  const limited = channel.max_running > 0;
  return {
    key: channel.key,
    name: channel.name,
    occupancy: `${channel.occupied} / ${limited ? channel.max_running : "不限"}`,
    waiting: channel.waiting,
    oldestWait:
      channel.waiting > 0 && channel.oldest_wait_sec > 0
        ? formatDuration(channel.oldest_wait_sec)
        : null,
    full: limited && channel.occupied >= channel.max_running,
  };
}

/**
 * “N 秒前”这样的相对时间；时钟略有偏差时不出现负数
 * @param iso ISO 时间字符串
 * @param now 当前时间（毫秒）
 * @returns 无法解析时为空串
 */
export function formatAgo(iso: string, now: number): string {
  const time = Date.parse(iso);
  if (Number.isNaN(time)) return "";
  const seconds = Math.max(0, Math.round((now - time) / 1000));
  return `${formatDuration(seconds)}前`;
}

/** 补零到两位 */
const pad = (value: number) => String(value).padStart(2, "0");

/**
 * 启动时间：今天显示“今天 09:12”，其他天显示“10-19 09:12”
 * @param iso ISO 时间字符串
 * @param now 当前时间（毫秒）
 * @returns 无法解析时为空串
 */
export function formatStartedAt(iso: string, now: number): string {
  const time = new Date(iso);
  if (Number.isNaN(time.getTime())) return "";
  const today = new Date(now);
  const clock = `${pad(time.getHours())}:${pad(time.getMinutes())}`;
  const sameDay =
    time.getFullYear() === today.getFullYear() &&
    time.getMonth() === today.getMonth() &&
    time.getDate() === today.getDate();
  return sameDay ? `今天 ${clock}` : `${pad(time.getMonth() + 1)}-${pad(time.getDate())} ${clock}`;
}

/**
 * 整分钟时间显示成“09:12”
 * @param iso ISO 时间字符串
 * @returns 无法解析时为空串
 */
export function formatClock(iso: string): string {
  const time = new Date(iso);
  return Number.isNaN(time.getTime()) ? "" : `${pad(time.getHours())}:${pad(time.getMinutes())}`;
}

/**
 * 实例在某个队列上的“在途 / 生效并发”；失联实例的在途数不可信，显示“–”
 * @param stat 后端给的该队列负载；没有时按 0
 * @param online 实例是否在线
 */
export function instanceLoadText(stat: InstanceQueueStat | undefined, online: boolean): string {
  if (!stat) return "–";
  return `${online ? stat.inflight : "–"}/${stat.limit}`;
}
