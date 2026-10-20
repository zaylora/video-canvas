import { useCallback } from "react";

import { getQueueOverview, getQueueThroughput } from "@/api/admin/queue";
import type { QueueInfo, QueueName } from "@/api/admin/queue/type";

import { usePolled } from "./use-polled";

/** 页面可见时每 5 秒刷新一次 */
export const QUEUE_POLL_MS = 5_000;

/** 曲线回看的分钟数 */
const THROUGHPUT_MINUTES = 60;

const fetchOverview = (silent: boolean) => getQueueOverview(silent);
const fetchThroughput = (silent: boolean) => getQueueThroughput(THROUGHPUT_MINUTES, silent);

/**
 * 任务队列页的数据：总览（队列、渠道占用、实例）与近 1 小时曲线，各自每 5 秒刷新。
 * 总览失败时整页显示失败；曲线失败只影响曲线卡片。
 * @returns 两份数据及其状态，以及写操作后对队列做本地修补的方法
 */
export function useQueueData() {
  const overview = usePolled(fetchOverview, QUEUE_POLL_MS);
  const throughput = usePolled(fetchThroughput, QUEUE_POLL_MS);
  const setOverview = overview.setData;
  const reloadOverview = overview.reload;
  const reloadThroughput = throughput.reload;

  /** 写操作成功后先把返回值落到界面上，再等下一次刷新校准，避免按钮“弹回去” */
  const patchQueue = useCallback(
    (name: QueueName, patch: Partial<QueueInfo>) =>
      setOverview((prev) =>
        prev
          ? { ...prev, queues: prev.queues.map((q) => (q.name === name ? { ...q, ...patch } : q)) }
          : prev,
      ),
    [setOverview],
  );

  /** 手动刷新两份数据 */
  const reloadAll = useCallback(
    () => Promise.all([reloadOverview(), reloadThroughput()]),
    [reloadOverview, reloadThroughput],
  );

  return { overview, throughput, patchQueue, reloadAll };
}
