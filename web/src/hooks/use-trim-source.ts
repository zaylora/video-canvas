import { useEffect, useRef, useState } from "react";

import { trimEngine } from "@/utils/canvas/trim-engine";
import {
  TrimError,
  describeTrimError,
  type TrimEngine,
  type TrimProbe,
} from "@/utils/canvas/trim-run";
import { thumbTimes, trimUrl } from "@/utils/canvas/trim";

/** 胶片条的缩略图格数：宽度大约 640px，每格约 40px 宽 */
const THUMB_COUNT = 16;
/** 缩略图高度（像素）：胶片条高 48，取两倍给高分屏 */
const THUMB_HEIGHT = 96;

export type TrimSourceState =
  | { status: "loading" }
  | { status: "ready"; probe: TrimProbe; thumbs: (string | null)[] }
  | { status: "error"; message: string };

/**
 * 修剪面板打开后读源视频：时长、有没有音轨、胶片条缩略图。
 * 先读信息（快），拿到后面板就能用，缩略图随后补上；任何一步失败都落成 error 状态并给话。
 * 卸载时中止读取并释放缩略图的 object URL，免得泄漏。
 * @param src 源视频地址；空地址不读
 * @param engine 读取用的引擎，默认走 Mediabunny；测试时注入假的
 */
export function useTrimSource(
  src: string | undefined,
  engine: TrimEngine = trimEngine,
): TrimSourceState {
  const [state, setState] = useState<TrimSourceState>({ status: "loading" });
  const urls = useRef<string[]>([]);

  useEffect(() => {
    if (!src) return;
    const controller = new AbortController();
    const revoke = () => {
      for (const url of urls.current) URL.revokeObjectURL(url);
      urls.current = [];
    };

    void (async () => {
      try {
        const read = trimUrl(src);
        const probe = await engine.probe(read, controller.signal);
        if (controller.signal.aborted) return;
        setState({ status: "ready", probe, thumbs: [] });
        const thumbs = await engine
          .thumbnails(
            read,
            thumbTimes(probe.duration, THUMB_COUNT),
            THUMB_HEIGHT,
            controller.signal,
          )
          .catch(() => Array.from({ length: THUMB_COUNT }, () => null) as (string | null)[]);
        if (controller.signal.aborted) {
          for (const url of thumbs) if (url) URL.revokeObjectURL(url);
          return;
        }
        urls.current = thumbs.filter((url): url is string => !!url);
        setState({ status: "ready", probe, thumbs });
      } catch (error) {
        if (controller.signal.aborted) return;
        setState({
          status: "error",
          message: error instanceof TrimError ? error.message : describeTrimError(error),
        });
      }
    })();

    return () => {
      controller.abort();
      revoke();
    };
  }, [engine, src]);

  return state;
}
