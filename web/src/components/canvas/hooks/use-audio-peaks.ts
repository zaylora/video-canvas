import { useEffect, useState } from "react";

import { computePeaks } from "@/utils/canvas/audio-peaks";

/** 超过这个大小的音频不解码取波形，退成圆点：解码要把整段读进内存 */
const MAX_DECODE_BYTES = 50 * 1024 * 1024;

/** 一段音频的解码结果 */
type AudioPeaks = {
  /** 每根竖条的 0~1 峰值；null 表示取不到 */
  peaks: number[] | null;
  /** 总时长，秒；没解出来为 0 */
  duration: number;
};

/** 已经算过的波形，按素材地址缓存；null 表示算过但取不到（太大、跨域、解码失败） */
const cache = new Map<string, AudioPeaks>();
const inflight = new Map<string, Promise<AudioPeaks>>();

let context: AudioContext | null = null;

/** 解码用的 AudioContext 全页面共用一个：浏览器限制同时存在的个数 */
function getContext(): AudioContext {
  context ??= new AudioContext();
  return context;
}

/** 下载并解码一段音频，取波形峰值；任何一步失败都返回 peaks: null，不抛错 */
async function decode(src: string, bars: number) {
  try {
    const response = await fetch(src);
    const length = Number(response.headers.get("content-length"));
    if (!response.ok || length > MAX_DECODE_BYTES) return { peaks: null, duration: 0 };
    const buffer = await getContext().decodeAudioData(await response.arrayBuffer());
    return { peaks: computePeaks(buffer.getChannelData(0), bars), duration: buffer.duration };
  } catch {
    return { peaks: null, duration: 0 };
  }
}

/**
 * 取一段音频的波形峰值，解码在加载后做、不阻塞渲染，同一地址只算一次。
 * @param src 音频地址
 * @param bars 要几根竖条
 * @returns peaks 为 undefined 表示还在解码；null 表示取不到，调用方画圆点；duration 是解码得到的总时长（秒），没有为 0
 */
export function useAudioPeaks(
  src: string,
  bars: number,
): { peaks: number[] | null | undefined; duration: number } {
  const key = `${bars}|${src}`;
  /** 解码完成的结果连同它对应的 key，换素材后旧结果不会串过来 */
  const [done, setDone] = useState<{ key: string; value: AudioPeaks } | null>(null);

  useEffect(() => {
    if (cache.has(key)) return;
    let cancelled = false;
    const job = inflight.get(key) ?? decode(src, bars);
    inflight.set(key, job);
    void job.then((value) => {
      cache.set(key, value);
      inflight.delete(key);
      if (!cancelled) setDone({ key, value });
    });
    return () => {
      cancelled = true;
    };
  }, [key, src, bars]);

  const result = cache.get(key) ?? (done?.key === key ? done.value : undefined);
  return { peaks: result?.peaks, duration: result?.duration ?? 0 };
}
