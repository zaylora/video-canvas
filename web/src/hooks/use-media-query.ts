import { useCallback, useSyncExternalStore } from "react";

/** 没有 matchMedia 的环境（测试、服务端） */
const noMatchMedia = () => typeof window === "undefined" || !window.matchMedia;

/**
 * 订阅一条媒体查询，窗口变化时跟着更新。
 * @param query 如 "(min-width: 1024px)"
 * @returns 当前是否命中；没有 matchMedia 的环境当作命中
 */
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onChange: () => void) => {
      if (noMatchMedia()) return () => {};
      const mql = window.matchMedia(query);
      mql.addEventListener("change", onChange);
      return () => mql.removeEventListener("change", onChange);
    },
    [query],
  );
  return useSyncExternalStore(
    subscribe,
    () => noMatchMedia() || window.matchMedia(query).matches,
    () => true,
  );
}
