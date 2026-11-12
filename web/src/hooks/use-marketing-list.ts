import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { deleteMarketingProject, listMarketingProjects } from "@/api/marketing";
import type { ProjectSummary } from "@/api/marketing/type";
import { useMarketingStore } from "@/store/marketing";

/**
 * 我的营销片列表：按更新时间倒序，只取第一页；删除成功后就地移除。
 * 有实时变化（写脚本结束、出片开始 / 结束）时静默重拉，卡片上的状态行和状态条跟着走。
 * @param pageSize 取几条
 */
export function useMarketingList(pageSize: number) {
  const [items, setItems] = useState<ProjectSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const version = useMarketingStore((state) => state.listVersion);
  const seen = useRef(version);

  const load = useCallback(
    async (silent: boolean) => {
      if (!silent) {
        setLoading(true);
        setFailed(false);
      }
      try {
        const page = await listMarketingProjects({ page: 1, pageSize });
        setItems(page.items);
        setTotal(page.total);
        setFailed(false);
        useMarketingStore
          .getState()
          .setTitles(Object.fromEntries(page.items.map((item) => [item.id, item.title])));
      } catch {
        if (!silent) setFailed(true);
      } finally {
        if (!silent) setLoading(false);
      }
    },
    [pageSize],
  );

  useEffect(() => {
    void load(false);
  }, [load]);

  /** 实时变化过后静默刷新；节流到下一个 tick，一批事件只拉一次 */
  useEffect(() => {
    if (seen.current === version) return;
    seen.current = version;
    const timer = window.setTimeout(() => void load(true), 400);
    return () => window.clearTimeout(timer);
  }, [version, load]);

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
    reload: () => void load(false),
    remove: (id: string) => void remove(id),
  };
}
