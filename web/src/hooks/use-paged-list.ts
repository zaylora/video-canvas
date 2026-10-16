import { useEffect, useMemo, useSyncExternalStore } from "react";

import {
  createPagedList,
  type PagedList,
  type PagedPage,
  type PagedState,
} from "@/utils/assets/paged-list";

/**
 * 把游标分页列表接进 React：key 变化（筛选条件变了）就换一个全新的列表，从首页开始加载；
 * 卸载或换列表时作废旧请求。
 * @param key 查询条件的稳定字符串
 * @param fetchPage 取一页，不传游标是首页；只在 key 变化时才会换新的
 * @returns 列表实例（给乐观更新用）与当前状态
 */
export function usePagedList<T>(
  key: string,
  fetchPage: (cursor: string | undefined) => Promise<PagedPage<T>>,
): { list: PagedList<T>; state: PagedState<T> } {
  // fetchPage 每次渲染都是新函数，但列表只该在 key 变化时重建
  // oxlint-disable-next-line react-hooks/exhaustive-deps
  const list = useMemo(() => createPagedList(fetchPage), [key]);
  useEffect(() => {
    list.load();
    return () => list.cancel();
  }, [list]);
  const state = useSyncExternalStore(list.subscribe, list.getState);
  return { list, state };
}
