import { useEffect, useState } from "react";

import { getMarketingOptions } from "@/api/marketing";
import type { MarketingOptions } from "@/api/marketing/type";
import { DEFAULT_ANGLES, DEFAULT_PLATFORMS } from "@/utils/marketing/options";

/** 兜底预设：接口没回来或失败时生成框照样能用 */
/** 接口没回来时的兜底；参考视频上限同后端默认（Seedance 2.x 为 15 秒） */
const FALLBACK: MarketingOptions = {
  platforms: DEFAULT_PLATFORMS,
  angles: DEFAULT_ANGLES,
  refVideoMaxSeconds: 15,
};

/** 取回来的预设整个会话只拉一次 */
let cached: MarketingOptions | null = null;
let inflight: Promise<MarketingOptions | null> | null = null;

const load = (): Promise<MarketingOptions | null> => {
  if (cached) return Promise.resolve(cached);
  inflight ??= getMarketingOptions()
    .then((options) => {
      if (options.platforms.length === 0) return null;
      cached = {
        platforms: options.platforms,
        angles: options.angles.length > 0 ? options.angles : DEFAULT_ANGLES,
        refVideoMaxSeconds: options.refVideoMaxSeconds ?? FALLBACK.refVideoMaxSeconds,
      };
      return cached;
    })
    .catch(() => null)
    .finally(() => {
      inflight = null;
    });
  return inflight;
};

/**
 * 投放平台与创意角度预设：先用内置兜底，接口回来后换成后端的（后端是唯一来源，运营可校对）。
 * @returns 平台列表和内置角度
 */
export function useMarketingOptions(): MarketingOptions {
  const [options, setOptions] = useState<MarketingOptions>(cached ?? FALLBACK);
  useEffect(() => {
    let alive = true;
    void load().then((loaded) => {
      if (alive && loaded) setOptions(loaded);
    });
    return () => {
      alive = false;
    };
  }, []);
  return options;
}
