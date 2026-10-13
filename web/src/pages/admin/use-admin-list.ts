import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";

import {
  clearListParams,
  parseListParams,
  toListQuery,
  withListParam,
  type ListParams,
} from "@/utils/admin/list-params";

import type { LoadStatus } from "./use-admin";

/** 没有数据时的空列表：引用稳定，依赖它的 effect 不会每次渲染都触发 */
const EMPTY: never[] = [];

/** 搜索框停止输入多久后才发请求 */
export const SEARCH_DEBOUNCE_MS = 300;

/** 列表默认每页条数 */
export const ADMIN_PAGE_SIZE = 20;

/** 后台分页接口的返回：当页的行、命中总数，可以带页面自己要的额外字段 */
export type AdminPageResult<T> = { list: T[]; total: number };

/**
 * 后台列表的服务端分页 + 搜索 + 筛选。
 * q / page / 筛选都在 URL 里（刷新不丢）；翻页和筛选时保留旧数据（refreshing 为 true），
 * 首次加载没有数据才是 loading；改搜索词或筛选时页码回到第 1 页；过期的响应会被丢掉。
 * @param filterKeys 本列表认的筛选参数名
 * @param fetcher 按查询参数请求当前页；每次渲染可以换身份，hook 内部取最新的
 */
export function useAdminList<T, R extends AdminPageResult<T> = AdminPageResult<T>>(
  filterKeys: readonly string[],
  fetcher: (query: Record<string, string | number>) => Promise<R>,
) {
  const [params, setParams] = useSearchParams();
  const keysKey = filterKeys.join(",");
  const list: ListParams = useMemo(
    () => parseListParams(params, keysKey ? keysKey.split(",") : []),
    [params, keysKey],
  );
  const [pageSize, setPageSizeState] = useState(ADMIN_PAGE_SIZE);
  const [result, setResult] = useState<R | null>(null);
  const [state, setState] = useState<LoadStatus>("loading");
  const [refreshing, setRefreshing] = useState(false);
  const [version, setVersion] = useState(0);
  const seq = useRef(0);
  const fetcherRef = useRef(fetcher);
  useEffect(() => {
    fetcherRef.current = fetcher;
  });

  // 请求只依赖这个字符串：参数没变就不重发
  const queryKey = JSON.stringify(toListQuery(list, pageSize));

  useEffect(() => {
    const mine = ++seq.current;
    setRefreshing(true);
    fetcherRef
      .current(JSON.parse(queryKey) as Record<string, string | number>)
      .then((next) => {
        if (mine !== seq.current) return;
        setResult(next);
        setState("ready");
      })
      .catch(() => {
        // 全局 toast 已由拦截器弹；有旧数据就留着，没有才显示失败态
        if (mine === seq.current) setState((prev) => (prev === "ready" ? prev : "error"));
      })
      .finally(() => {
        if (mine === seq.current) setRefreshing(false);
      });
  }, [queryKey, version]);

  const setParam = useCallback(
    (key: string, value: string | null) =>
      setParams((prev) => withListParam(prev, key, value), { replace: true }),
    [setParams],
  );
  const setQ = useCallback((value: string) => setParam("q", value), [setParam]);
  const setFilter = useCallback((key: string, value: string) => setParam(key, value), [setParam]);
  const setPage = useCallback((page: number) => setParam("page", String(page)), [setParam]);
  const setPageSize = useCallback(
    (size: number) => {
      setPageSizeState(size);
      setParam("page", null);
    },
    [setParam],
  );
  /** 清掉搜索词和本列表的筛选，回到第 1 页；URL 里的其它参数（弹窗的 ?key= 等）保留 */
  const clear = useCallback(
    () =>
      setParams((prev) => clearListParams(prev, keysKey ? keysKey.split(",") : []), {
        replace: true,
      }),
    [setParams, keysKey],
  );
  /** 重新拉取当前页；不清空旧数据 */
  const reload = useCallback(() => setVersion((v) => v + 1), []);

  /** 就地改当前已取到的行（乐观更新用）；之后 reload 会用后端数据覆盖 */
  const patchItems = useCallback(
    (fn: (items: T[]) => T[]) =>
      setResult((prev) => (prev ? { ...prev, list: fn(prev.list) } : prev)),
    [],
  );

  return {
    ...list,
    pageSize,
    items: result?.list ?? (EMPTY as T[]),
    total: result?.total ?? 0,
    /** 接口返回的整份结果（取额外字段用，如存储的 default_id） */
    result,
    state,
    refreshing,
    /** 有搜索词或筛选 */
    filtering: list.q.trim() !== "" || Object.keys(list.filters).length > 0,
    setQ,
    setFilter,
    setPage,
    setPageSize,
    clear,
    reload,
    patchItems,
  };
}

/**
 * 搜索框的输入值：输入立即显示，停 300ms 才把值交给 onCommit（发请求）；
 * URL 里的值变化（清除筛选、后退）时输入框跟着变。
 * @param committed 已生效的搜索词（来自 URL）
 */
export function useDebouncedSearch(committed: string, onCommit: (value: string) => void) {
  const [input, setInput] = useState(committed);
  useEffect(() => setInput(committed), [committed]);
  useEffect(() => {
    if (input === committed) return;
    const timer = setTimeout(() => onCommit(input), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [input, committed, onCommit]);
  return [input, setInput] as const;
}
