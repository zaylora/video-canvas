import { useEffect, useState } from "react";

import { getDramaStyles } from "@/api/drama";
import type { DramaStyles } from "@/api/drama/type";

const EMPTY: DramaStyles = { genres: [], styles: [] };

/** 题材与画风清单几乎不变，整个会话只拉一次 */
let cached: DramaStyles | null = null;
let inflight: Promise<DramaStyles> | null = null;

const load = (): Promise<DramaStyles> => {
  inflight ??= getDramaStyles()
    .then((styles) => {
      cached = styles;
      return styles;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
};

/**
 * 题材与画风清单（契约 `GET /drama-styles`）。
 * 加载失败时返回空清单，调用方显示空选项；下次进入页面会重试。
 */
export function useDramaStyles(): DramaStyles & { ready: boolean } {
  const [styles, setStyles] = useState<DramaStyles | null>(cached);
  useEffect(() => {
    if (cached) return;
    let active = true;
    void load()
      .then((value) => active && setStyles(value))
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, []);
  return { ...(styles ?? EMPTY), ready: styles !== null };
}
