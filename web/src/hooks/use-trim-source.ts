import { useEffect, useState } from "react";

import { trimEngine } from "@/utils/canvas/trim-engine";
import type { TrimEngine } from "@/utils/canvas/trim-run";
import {
  cachedTrimSource,
  loadTrimSource,
  trimSourceCache,
  type TrimSourceState,
} from "@/utils/canvas/trim-source";

export type { TrimSourceState };

/**
 * 修剪、截帧面板打开后读源视频：时长、有没有音轨、胶片条缩略图（读取和缓存规则见 loadTrimSource）。
 * 读过的视频直接从缓存出结果，再打开面板不用重新等；卸载时中止还没读完的部分。
 * @param src 源视频地址；空地址不读
 * @param engine 读取用的引擎，默认走 Mediabunny；测试时注入假的
 */
export function useTrimSource(
  src: string | undefined,
  engine: TrimEngine = trimEngine,
): TrimSourceState {
  const [state, setState] = useState<TrimSourceState>(
    () => (src ? cachedTrimSource(src, trimSourceCache) : undefined) ?? { status: "loading" },
  );

  useEffect(() => {
    if (!src) return;
    const controller = new AbortController();
    void loadTrimSource({
      src,
      engine,
      cache: trimSourceCache,
      signal: controller.signal,
      onState: setState,
    });
    return () => controller.abort();
  }, [engine, src]);

  return state;
}
