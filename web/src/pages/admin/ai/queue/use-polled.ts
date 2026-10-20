import { useCallback, useEffect, useRef, useState } from "react";

import { useAliveRef, type LoadStatus } from "../../use-admin";
import { useVisibleInterval } from "../overview/use-visible-interval";

/**
 * 页面可见时按固定间隔刷新一份数据（任务队列页的总览与吞吐曲线共用）。
 * - 首次加载与手动重试失败由全局 toast 告知；定时刷新静默失败，已有数据时保留旧数据并标记 stale；
 * - 只认最新那次请求的结果，返回得晚的旧请求直接丢弃；
 * - 返回的 data 与调用方自己保存的草稿是分开的，刷新不会碰草稿。
 * @param fetcher 取数函数，必须引用稳定（模块级函数）；silent 为 true 时失败不弹全局提示
 * @param intervalMs 刷新间隔（毫秒）
 * @returns 数据、加载状态、最近一次刷新是否失败、取到数据的时间、手动刷新（失败态下重试）、静默刷新、本地修补数据
 */
export function usePolled<T>(fetcher: (silent: boolean) => Promise<T>, intervalMs: number) {
  const aliveRef = useAliveRef();
  const [data, setData] = useState<T | null>(null);
  const [status, setStatus] = useState<LoadStatus>("loading");
  const [stale, setStale] = useState(false);
  const [fetchedAt, setFetchedAt] = useState(() => Date.now());
  /** 最近一次请求的序号：只认最新那次的结果 */
  const seq = useRef(0);

  const load = useCallback(
    async (silent: boolean) => {
      const id = ++seq.current;
      try {
        const next = await fetcher(silent);
        if (!aliveRef.current || id !== seq.current) return;
        setData(next);
        setStatus("ready");
        setStale(false);
        setFetchedAt(Date.now());
      } catch {
        if (!aliveRef.current || id !== seq.current) return;
        setStatus((prev) => (prev === "ready" ? prev : "error"));
        setStale(true);
      }
    },
    [aliveRef, fetcher],
  );

  useEffect(() => {
    void load(false);
  }, [load]);
  useVisibleInterval(() => void load(true), intervalMs);

  /** 手动刷新：失败态下先回到加载中，让页面显示骨架而不是一直停在失败 */
  const reload = useCallback(() => {
    setStatus((prev) => (prev === "error" ? "loading" : prev));
    return load(false);
  }, [load]);

  /** 写操作成功后静默刷新，不弹提示 */
  const refreshSilently = useCallback(() => load(true), [load]);

  return { data, status, stale, fetchedAt, reload, refreshSilently, setData };
}
