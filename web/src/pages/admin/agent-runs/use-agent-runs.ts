import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { listAgentRuns } from "@/api/admin/agent-run";
import type { AgentRunDigest } from "@/api/admin/agent-run/type.d";
import {
  clampPage,
  DEFAULT_RUN_FILTERS,
  hasActiveRunFilters,
  runListQuery,
  toggleHarnessFilter,
  type RunFilters,
} from "@/utils/admin/agent-run";

import type { RunListState } from "./run-list";

/** 默认每页条数 */
export const RUN_PAGE_SIZE = 20;

/** 没有数据时的空列表：引用稳定 */
const EMPTY: AgentRunDigest[] = [];

/**
 * 运行记录列表：筛选条件 + 页码分页。
 * 筛选变化回到第 1 页；翻页和换筛选时保留旧数据（refreshing 为 true，页面半透明），首次没有数据才是 loading；
 * 过期的响应会被丢掉。时间预设的起点在筛选变化的那一刻算定，翻页和重试都沿用同一个起点，前后页不会因为时间流逝对不上。
 * 总数变少导致当前页越界（例如最后一页的记录被清理）时自动回到最后一页。
 * @returns 筛选条件与修改方法、列表数据与分页状态
 */
export function useAgentRuns() {
  const [{ filters, at }, setFilterState] = useState(() => ({
    filters: DEFAULT_RUN_FILTERS,
    at: Date.now(),
  }));
  const [page, setPage] = useState(1);
  const [pageSize, setPageSizeState] = useState(RUN_PAGE_SIZE);
  const [items, setItems] = useState<AgentRunDigest[]>(EMPTY);
  const [total, setTotal] = useState(0);
  const [state, setState] = useState<RunListState>("loading");
  const [refreshing, setRefreshing] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const seq = useRef(0);

  const query = useMemo(
    () => runListQuery(filters, at, { page, pageSize }),
    [filters, at, page, pageSize],
  );
  // 请求只依赖这个字符串：参数没变就不重发
  const queryKey = JSON.stringify(query);

  useEffect(() => {
    const mine = ++seq.current;
    setRefreshing(true);
    listAgentRuns(JSON.parse(queryKey) as typeof query)
      .then((result) => {
        if (mine !== seq.current) return;
        const nextTotal = result.total ?? result.items.length;
        const requested = (JSON.parse(queryKey) as typeof query).page ?? 1;
        const fixed = clampPage(requested, nextTotal, pageSize);
        if (fixed !== requested) {
          setPage(fixed); // 越界：回到最后一页，由页码变化触发重新请求
          return;
        }
        setItems(result.items);
        setTotal(nextTotal);
        setState("ready");
      })
      .catch(() => {
        // 全局 toast 已弹；有旧数据就留着，没有才显示失败态
        if (mine === seq.current) setState((prev) => (prev === "ready" ? prev : "error"));
      })
      .finally(() => {
        if (mine === seq.current) setRefreshing(false);
      });
    // pageSize 已包含在 queryKey 里（limit），这里不再单独依赖
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queryKey, attempt]);

  /** 改筛选：同时记下此刻作为时间预设的起点，并回到第 1 页 */
  const update = useCallback((change: (prev: RunFilters) => RunFilters) => {
    const now = Date.now();
    setFilterState((prev) => ({ filters: change(prev.filters), at: now }));
    setPage(1);
  }, []);
  const patch = useCallback(
    (next: Partial<RunFilters>) => update((prev) => ({ ...prev, ...next })),
    [update],
  );
  const reset = useCallback(() => update(() => DEFAULT_RUN_FILTERS), [update]);
  const toggleHarness = useCallback(
    (key: string) => update((prev) => toggleHarnessFilter(prev, key)),
    [update],
  );
  const setPageSize = useCallback((size: number) => {
    setPageSizeState(size);
    setPage(1);
  }, []);
  /** 重新拉取当前页；失败态下点「重试」，不清空旧数据 */
  const retry = useCallback(() => {
    setState((prev) => (prev === "error" ? "loading" : prev));
    setAttempt((n) => n + 1);
  }, []);

  return {
    filters,
    active: hasActiveRunFilters(filters),
    patch,
    reset,
    toggleHarness,
    list: { items, total, state, refreshing, page, pageSize, setPage, setPageSize, retry },
  };
}
