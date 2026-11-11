import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { getAnnouncements } from "@/api/announcement";
import type { AnnouncementDto } from "@/api/announcement/type";
import {
  CACHE_KEY,
  DISMISSED_KEY,
  dismiss,
  parseCachedAnnouncements,
  parseDismissed,
  shouldRefetch,
  visibleAnnouncements,
  type DismissedList,
} from "@/utils/home/announcement";

/** 取存储对象放在函数里：访问 window.localStorage 本身在部分隐私模式下就会抛错，要落在 try 里 */
const local = () => localStorage;
const session = () => sessionStorage;

/** 每 30 秒本地检查一次结束时间：维护结束了，不等下一次拉取就该消失 */
const EXPIRY_TICK_MS = 30_000;

/**
 * 读浏览器存储；隐私模式等读不了时当作没有
 * @param store 公告缓存用 localStorage（跨会话，首屏不闪），关闭记录用 sessionStorage（只在当前会话有效）
 */
function readStorage(store: () => Storage, key: string): string | null {
  try {
    return store().getItem(key);
  } catch {
    return null;
  }
}

/** 写浏览器存储；写不进去只是下次不记得，不影响使用 */
function writeStorage(store: () => Storage, key: string, value: unknown) {
  try {
    store().setItem(key, JSON.stringify(value));
  } catch {
    // 退回只在本次会话的内存里生效
  }
}

/**
 * 首页公告的数据（设计稿 docs/design/公告/公告.md §6.1、§7）：
 * - 首屏先用本机缓存的上一次公告渲染，再静默拉取最新的，避免每次进首页整页往下跳；
 * - 回到窗口（重新获得焦点 / 标签页重新可见）时，距上次拉取满 5 分钟才再拉；
 * - 拉取失败保留上一次的内容，不弹提示；
 * - 每 30 秒按结束时间过滤一次；
 * - 关闭只在当前会话有效：关闭记录（id + 版本）存在 sessionStorage，同一标签页刷新后仍不显示，
 *   关掉标签页 / 浏览器或新开标签页后会重新看到；读写不了时只在内存里生效。
 * @returns visible 当前要显示的公告；close 关闭一条（只在当前会话有效）
 */
export function useAnnouncements() {
  const [items, setItems] = useState<AnnouncementDto[]>(() =>
    parseCachedAnnouncements(readStorage(local, CACHE_KEY)),
  );
  const [dismissed, setDismissed] = useState<DismissedList>(() =>
    parseDismissed(readStorage(session, DISMISSED_KEY)),
  );
  const [now, setNow] = useState(() => Date.now());
  const lastFetchRef = useRef<number | null>(null);
  const inflightRef = useRef(false);

  const refresh = useCallback(async () => {
    if (inflightRef.current) return;
    inflightRef.current = true;
    try {
      const list = await getAnnouncements();
      lastFetchRef.current = Date.now();
      setItems(list);
      setNow(Date.now());
      writeStorage(local, CACHE_KEY, list);
    } catch {
      // 静默失败：保留缓存或上一次成功的内容
    } finally {
      inflightRef.current = false;
    }
  }, []);

  useEffect(() => {
    void refresh();
    const onReturn = () => {
      if (document.visibilityState !== "visible") return;
      setNow(Date.now());
      if (shouldRefetch(lastFetchRef.current, Date.now())) void refresh();
    };
    const timer = window.setInterval(() => setNow(Date.now()), EXPIRY_TICK_MS);
    window.addEventListener("focus", onReturn);
    document.addEventListener("visibilitychange", onReturn);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", onReturn);
      document.removeEventListener("visibilitychange", onReturn);
    };
  }, [refresh]);

  const close = useCallback((item: AnnouncementDto) => {
    setDismissed((prev) => {
      const next = dismiss(prev, item.id, item.version);
      writeStorage(session, DISMISSED_KEY, next);
      return next;
    });
  }, []);

  const visible = useMemo(
    () => visibleAnnouncements(items, dismissed, now),
    [items, dismissed, now],
  );

  return { visible, close };
}
