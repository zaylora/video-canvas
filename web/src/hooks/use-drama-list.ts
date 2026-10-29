import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { deleteDrama, listDramas } from "@/api/drama";
import type { DramaSummary } from "@/api/drama/type";

/**
 * 我的短剧列表：按更新时间倒序分页，可按标题搜索，删除成功后就地移除。
 * 短剧工作室首页（只取前几部）和作品页的「个人短剧」共用。
 * 关键词变了从第一页重新拉；慢的旧请求回来时如果关键词已经变了就丢掉，不覆盖新结果。
 * @param pageSize 每页条数
 * @param keyword 搜索关键词（调用方自行防抖）
 */
export function useDramaList(pageSize: number, keyword = "") {
  const [items, setItems] = useState<DramaSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  /** 最近一次请求的关键词，用来丢掉过时的响应 */
  const latest = useRef(keyword);

  const load = useCallback(
    async (next: number) => {
      latest.current = keyword;
      setLoading(true);
      setFailed(false);
      try {
        const result = await listDramas({ page: next, pageSize, keyword: keyword || undefined });
        if (latest.current !== keyword) return;
        setItems((prev) => (next === 1 ? result.items : [...prev, ...result.items]));
        setTotal(result.total);
        setPage(next);
      } catch {
        if (latest.current === keyword) setFailed(true);
      } finally {
        if (latest.current === keyword) setLoading(false);
      }
    },
    [pageSize, keyword],
  );

  useEffect(() => {
    void load(1);
  }, [load]);

  const remove = useCallback(async (id: string) => {
    setDeletingId(id);
    try {
      await deleteDrama(id);
      setItems((prev) => prev.filter((item) => item.id !== id));
      setTotal((prev) => Math.max(0, prev - 1));
      toast.success("已删除");
    } catch {
      // 请求层已经弹了错误提示
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
