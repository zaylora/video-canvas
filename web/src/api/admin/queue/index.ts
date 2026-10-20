import service from "@/utils/requests/service";
import type {
  PauseResult,
  QueueName,
  QueueOverview,
  RiverTicket,
  SaveConcurrencyResult,
  ThroughputPoint,
} from "./type";

/**
 * 接口路径集中在这里（前缀 /api/v1 由 axios baseURL 提供），契约见设计文档 docs/design/任务队列River：
 *
 *   GET  /admin/queue/overview               队列、渠道占用、实例（admin 可读）
 *   GET  /admin/queue/throughput?minutes=    按分钟的完成数 / 失败数 / 平均排队时长
 *   PUT  /admin/queue/queues/:name           保存并发，body = {concurrency}（super_admin）
 *   POST /admin/queue/queues/:name/pause     暂停（super_admin，幂等）
 *   POST /admin/queue/queues/:name/resume    恢复（super_admin，幂等）
 *   POST /admin/queue/river-ticket           换取 River UI 的一次性入口（super_admin）
 */
const P = "/admin/queue";

/**
 * 任务队列总览：三个队列的计数与并发、渠道占用、工作实例
 * @param silent 为 true 时失败不弹全局提示：定时刷新用，页面自己显示“刷新失败”
 * @returns 总览；数组字段缺失时补成空数组
 */
export const getQueueOverview = async (silent = false): Promise<QueueOverview> => {
  const overview = await service.get<QueueOverview>(
    `${P}/overview`,
    undefined,
    silent ? { silent: true } : undefined,
  );
  return {
    queues: overview.queues ?? [],
    channels: overview.channels ?? [],
    instances: (overview.instances ?? []).map((instance) => ({
      ...instance,
      stats: instance.stats ?? {},
    })),
  };
};

/**
 * 按分钟的吞吐曲线数据
 * @param minutes 回看多少分钟，15 到 1440
 * @param silent 为 true 时失败不弹全局提示：定时刷新用
 * @returns 每分钟的完成数、失败数、平均排队时长；后端已把没有任务的分钟补成 0
 */
export const getQueueThroughput = async (minutes = 60, silent = false) =>
  (await service.get<ThroughputPoint[] | null>(
    `${P}/throughput`,
    { minutes },
    silent ? { silent: true } : undefined,
  )) ?? [];

/**
 * 保存某个队列的并发（super_admin）；越界返回 400
 * @param name 队列名称
 * @param concurrency 新的并发，1 到该队列的硬上限
 * @returns 保存后的并发，以及已生效的实例数 / 总实例数
 */
export const saveQueueConcurrency = (name: QueueName, concurrency: number) =>
  service.put<SaveConcurrencyResult>(`${P}/queues/${name}`, { concurrency });

/**
 * 暂停队列（super_admin）：已入队的任务保留，对所有实例生效；重复调用无副作用
 * @param name 队列名称
 * @returns 操作后的暂停状态
 */
export const pauseQueue = (name: QueueName) =>
  service.post<PauseResult>(`${P}/queues/${name}/pause`);

/**
 * 恢复队列（super_admin）；重复调用无副作用
 * @param name 队列名称
 * @returns 操作后的暂停状态
 */
export const resumeQueue = (name: QueueName) =>
  service.post<PauseResult>(`${P}/queues/${name}/resume`);

/**
 * 换取 River UI 的一次性入口（super_admin）：浏览器直接打开页面带不上 Bearer 头，所以先换 ticket
 * @returns 同源相对地址，ticket 30 秒内有效，只能用一次
 */
export const createRiverTicket = () => service.post<RiverTicket>(`${P}/river-ticket`);
