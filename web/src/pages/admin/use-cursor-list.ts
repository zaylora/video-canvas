import { useCallback, useEffect, useRef, useState } from "react";

import type { CursorPage } from "@/api/admin/users/type.d";

type ListLoad = "loading" | "ready" | "error";

/**
 * 游标分页列表：首屏加载、「加载更多」追加、失败可重试。
 * key 变化（换用户、换筛选、外部刷新）就从头加载，旧请求的结果会被丢弃。
 * 用户详情的各页签和后台的使用记录、操作审计共用。
 * @param key 请求身份，变化时重新加载
 * @param load 取一页；cursor 为 null 表示第一页
 */
export function useCursorList<T>(
  key: string,
  load: (cursor: string | null) => Promise<CursorPage<T>>,
) {
  const [items, setItems] = useState<T[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [state, setState] = useState<ListLoad>("loading");
  const [loadingMore, setLoadingMore] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const seq = useRef(0);
  const loadRef = useRef(load);
  useEffect(() => {
    loadRef.current = load;
  });

  useEffect(() => {
    const mine = ++seq.current;
    setState("loading");
    setItems([]);
    setCursor(null);
    loadRef
      .current(null)
      .then((page) => {
        if (mine !== seq.current) return;
        setItems(page.items);
        setCursor(page.next_cursor);
        setState("ready");
      })
      .catch(() => {
        // 全局 toast 已弹；页签里给「加载失败 · 重试」
        if (mine === seq.current) setState("error");
      });
  }, [key, attempt]);

  const more = useCallback(() => {
    if (!cursor || loadingMore) return;
    const mine = seq.current;
    setLoadingMore(true);
    loadRef
      .current(cursor)
      .then((page) => {
        if (mine !== seq.current) return;
        setItems((prev) => [...prev, ...page.items]);
        setCursor(page.next_cursor);
      })
      .catch(() => undefined)
      .finally(() => setLoadingMore(false));
  }, [cursor, loadingMore]);

  return {
    items,
    state,
    hasMore: cursor !== null,
    loadingMore,
    more,
    retry: () => setAttempt((n) => n + 1),
  };
}
