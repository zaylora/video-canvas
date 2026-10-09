import { useEffect, useRef } from "react";

/**
 * 页面可见时每隔 delayMs 调一次 callback；标签页被切走时暂停，切回来立刻补一次。
 * 总览的统计与渠道负载靠它轮询：后台标签页不该白白打请求。
 * @param callback 要定时执行的函数，始终用最新的那个，不用 useCallback 包
 * @param delayMs 间隔（毫秒）
 */
export function useVisibleInterval(callback: () => void, delayMs: number) {
  const latest = useRef(callback);
  useEffect(() => {
    latest.current = callback;
  });
  useEffect(() => {
    const run = () => {
      if (!document.hidden) latest.current();
    };
    const timer = window.setInterval(run, delayMs);
    document.addEventListener("visibilitychange", run);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", run);
    };
  }, [delayMs]);
}
