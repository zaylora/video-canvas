import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { deleteMarketingProject, listMarketingProjects } from "@/api/marketing";
import type { ProjectSummary } from "@/api/marketing/type";
import { useMarketingStore } from "@/store/marketing";

/**
 * 我的营销片列表：按更新时间倒序分页，可按标题搜索，删除成功后就地移除。
 * 营销工作室首页、创作页「营销」tab（只取前几条）和作品页的「个人营销片」共用。
 * 有实时变化（写脚本结束、出片开始 / 结束）时静默重拉已加载的这些条，卡片上的状态行和状态条跟着走。
 * 关键词变了从第一页重新拉；慢的旧请求回来时如果关键词已经变了就丢掉，不覆盖新结果。
 * @param pageSize 每页条数
 * @param keyword 搜索关键词（调用方自行防抖）
 */
export function useMarketingList(pageSize: number, keyword = "") {
  const [items, setItems] = useState<ProjectSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const version = useMarketingStore((state) => state.listVersion);
  const seen = useRef(version);
  /** 最近一次请求的关键词，用来丢掉过时的响应 */
  const latest = useRef(keyword);
  /** 已加载到第几页，静默刷新时一次拉回这么多条 */
  const loaded = useRef(1);

  const load = useCallback(
    async (next: number) => {
      latest.current = keyword;
      setLoading(true);
      setFailed(false);
      try {
        const result = await listMarketingProjects({
          page: next,
          pageSize,
          keyword: keyword || undefined,
        });
        if (latest.current !== keyword) return;
        setItems((prev) => (next === 1 ? result.items : [...prev, ...result.items]));
        setTotal(result.total);
        setPage(next);
        loaded.current = next;
        useMarketingStore
          .getState()
          .setTitles(Object.fromEntries(result.items.map((item) => [item.id, item.title])));
      } catch {
        if (latest.current === keyword) setFailed(true);
      } finally {
        if (latest.current === keyword) setLoading(false);
      }
    },
    [pageSize, keyword],
  );

  /** 静默刷新：把已加载的几页重拉一遍（后端每页有上限，不能合成一页拉），不动加载态，失败就保持原样 */
  const refresh = useCallback(async () => {
    try {
      const pages = await Promise.all(
        Array.from({ length: loaded.current }, (_, i) =>
          listMarketingProjects({ page: i + 1, pageSize, keyword: keyword || undefined }),
        ),
      );
      if (latest.current !== keyword) return;
      const fresh = pages.flatMap((result) => result.items);
      setItems(fresh);
      setTotal(pages[0]?.total ?? 0);
      useMarketingStore
        .getState()
        .setTitles(Object.fromEntries(fresh.map((item) => [item.id, item.title])));
    } catch {
      // 静默刷新失败不打扰用户，下一次实时变化再拉
    }
  }, [pageSize, keyword]);

  useEffect(() => {
    void load(1);
  }, [load]);

  /** 实时变化过后静默刷新；节流到下一个 tick，一批事件只拉一次 */
  useEffect(() => {
    if (seen.current === version) return;
    seen.current = version;
    const timer = window.setTimeout(() => void refresh(), 400);
    return () => window.clearTimeout(timer);
  }, [version, refresh]);

  const remove = useCallback(async (id: string) => {
    setDeletingId(id);
    try {
      await deleteMarketingProject(id);
      setItems((prev) => prev.filter((item) => item.id !== id));
      setTotal((prev) => Math.max(0, prev - 1));
      useMarketingStore.getState().drop(id);
      toast.success("已删除");
    } catch {
      // 请求层已经弹了错误提示（68010 有进行中的任务不能删除）
    } finally {
      setDeletingId(null);
    }
  }, []);

  return {
    items,
    total,
    loading,
    failed,
    deletingId,
    hasMore: items.length < total,
    reload: () => void load(1),
    loadMore: () => void load(page + 1),
    remove: (id: string) => void remove(id),
  };
}
