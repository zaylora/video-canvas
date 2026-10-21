import { useCallback, useMemo, useState } from "react";

import { listAgentRuns } from "@/api/admin/agent-run";
import type { AgentRunDigest } from "@/api/admin/agent-run/type.d";
import {
  DEFAULT_RUN_FILTERS,
  hasActiveRunFilters,
  runListQuery,
  toCursorPage,
  toggleHarnessFilter,
  type RunFilters,
} from "@/utils/admin/agent-run";

import { useCursorList } from "../use-cursor-list";

/** 每页条数 */
export const RUN_PAGE_SIZE = 20;

/**
 * 运行记录列表：筛选条件 + 游标分页。
 * 任一筛选变化，key 随之变化，列表就丢掉旧数据从第一页重新加载（旧请求的结果会被丢弃）。
 * 时间预设的起点在筛选变化的那一刻算定，翻页和重试都沿用同一个起点，不会因为时间流逝让前后页对不上。
 * @returns 筛选条件与修改方法、列表状态
 */
export function useAgentRuns() {
  const [{ filters, at }, setState] = useState(() => ({
    filters: DEFAULT_RUN_FILTERS,
    at: Date.now(),
  }));
  const query = useMemo(() => runListQuery(filters, at), [filters, at]);
  const list = useCursorList<AgentRunDigest>(JSON.stringify(filters), async (cursor) =>
    toCursorPage(
      await listAgentRuns({ ...query, limit: RUN_PAGE_SIZE, ...(cursor ? { cursor } : {}) }),
    ),
  );

  /** 改筛选：同时记下此刻，作为时间预设的起点 */
  const update = useCallback((change: (prev: RunFilters) => RunFilters) => {
    const now = Date.now();
    setState((prev) => ({ filters: change(prev.filters), at: now }));
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

  return { filters, active: hasActiveRunFilters(filters), patch, reset, toggleHarness, list };
}
