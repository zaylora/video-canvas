import { useEffect, useState } from "react";

/** 以固定节拍刷新的当前时间，active 为 false 时不走表，用来给「已耗时」计时 */
export function useNow(active: boolean, intervalMs = 1000) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(timer);
  }, [active, intervalMs]);

  return now;
}
