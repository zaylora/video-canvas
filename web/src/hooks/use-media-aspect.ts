import { useEffect, useState } from "react";

import type { NodeMediaType } from "@/types";
import { probeMediaAspect } from "@/utils/canvas/media-aspect";

/** 已经量过的素材画幅，按素材地址缓存：重渲染、撤销、切回旧版本都不用再量一遍 */
const measuredAspects = new Map<string, number>();

/**
 * 量出节点里素材的真实宽高比，让节点框能和成片对上。
 * 量到之前返回 null，调用方先用别的画幅兜底；地址变了（换版本、重新生成）会重新量。
 * @param src 素材地址；没有（还没产出）就不量
 * @param mediaType 素材种类；音频没有画面，不量
 * @returns 宽 / 高；还没量到或量不出来为 null
 */
export function useMediaAspect(
  src: string | null | undefined,
  mediaType: NodeMediaType | undefined,
): number | null {
  const key = src && mediaType && mediaType !== "audio" ? src : null;
  const [probed, setProbed] = useState<{ key: string; aspect: number } | null>(null);

  useEffect(() => {
    if (!key || !mediaType || measuredAspects.has(key)) return;
    let cancelled = false;
    void probeMediaAspect(key, mediaType).then((aspect) => {
      if (aspect === null) return;
      measuredAspects.set(key, aspect);
      if (!cancelled) setProbed({ key, aspect });
    });
    return () => {
      cancelled = true;
    };
  }, [key, mediaType]);

  if (!key) return null;
  return measuredAspects.get(key) ?? (probed?.key === key ? probed.aspect : null);
}
