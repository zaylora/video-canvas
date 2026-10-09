import { useCallback, useEffect, useRef, useState } from "react";

import { getAdminStats, listChannelLoads } from "@/api/admin/ai";
import type { AdminStats, AdminStatsDays, ChannelLoad } from "@/api/admin/ai/type";

import { useAliveRef, type LoadStatus } from "../../use-admin";
import { useVisibleInterval } from "./use-visible-interval";

/** 任务统计每分钟刷新一次 */
const STATS_POLL_MS = 60_000;
/** 渠道负载每 15 秒刷新一次 */
const LOADS_POLL_MS = 15_000;

/**
 * 总览的任务统计：范围（7 / 30 天）、数据与加载状态。
 * - 切换范围先回到加载中，免得拿旧范围的数据画新范围的图；返回得晚的旧请求直接丢弃；
 * - 定时刷新静默失败（不弹 toast），已有数据时保留旧数据；首次加载或手动刷新失败由全局提示告知，页面显示失败状态；
 * @returns 范围与切换函数、当前范围的数据、加载状态、手动刷新
 */
export function useOverviewStats() {
  const aliveRef = useAliveRef();
  const [days, setDays] = useState<AdminStatsDays>(7);
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [status, setStatus] = useState<LoadStatus>("loading");
  /** 最近一次请求的序号：只认最新那次的结果 */
  const seq = useRef(0);

  const load = useCallback(
    async (target: AdminStatsDays, silent: boolean) => {
      const id = ++seq.current;
      try {
        const next = await getAdminStats(target, silent);
        if (!aliveRef.current || id !== seq.current) return;
        setStats(next);
        setStatus("ready");
      } catch {
        if (!aliveRef.current || id !== seq.current) return;
        setStatus((prev) => (prev === "ready" ? prev : "error"));
      }
    },
    [aliveRef],
  );

  useEffect(() => {
    void load(days, false);
  }, [days, load]);
  useVisibleInterval(() => void load(days, true), STATS_POLL_MS);

  const changeDays = useCallback((next: AdminStatsDays) => {
    setDays(next);
    setStats(null);
    setStatus("loading");
  }, []);
  const reload = useCallback(() => load(days, false), [days, load]);

  return { days, changeDays, stats, status, reload };
}

/**
 * 各渠道的实时负载：每 15 秒轮询。
 * 失败不弹 toast（轮询每次都弹会很吵），已有数据时保留并标记 stale，由负载卡显示“刷新失败，显示的是旧数据”。
 * @returns 负载、加载状态、最近一次刷新是否失败、手动刷新
 */
export function useChannelLoads() {
  const aliveRef = useAliveRef();
  const [loads, setLoads] = useState<ChannelLoad[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");
  const [stale, setStale] = useState(false);

  const reload = useCallback(async () => {
    try {
      const next = await listChannelLoads(true);
      if (!aliveRef.current) return;
      setLoads(next);
      setStatus("ready");
      setStale(false);
    } catch {
      if (!aliveRef.current) return;
      setStatus((prev) => (prev === "ready" ? prev : "error"));
      setStale(true);
    }
  }, [aliveRef]);

  useEffect(() => {
    void reload();
  }, [reload]);
  useVisibleInterval(() => void reload(), LOADS_POLL_MS);

  return { loads, status, stale, reload };
}
