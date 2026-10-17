import { useCallback, useState } from "react";

/**
 * 视频元素读取 /files/ 地址时的跨域策略：先带 crossOrigin="anonymous"，失败了去掉它再试一次。
 *
 * 为什么先带：截帧、修剪要在 WebCodecs 里读同一个文件，必须带跨域头；浏览器按完整地址缓存响应，
 * 播放若不带跨域头，缓存里就是一份没有 Access-Control-Allow-Origin 的响应，跨域读取复用它就被 CORS 拦下。
 * 播放也带，两边的请求一样，才能共用一份缓存。
 * 为什么能退回：存储或外链没配跨域头时，带 crossOrigin 的视频会加载失败；去掉后至少还能播，只是截帧、修剪用不了。
 *
 * 换了地址会重新从带跨域头开始。
 * @param src 视频地址
 * @param onFail 去掉跨域头后仍然失败时调用
 * @returns crossOrigin 设给 video 元素（要在设 src 之前），onError 接 video 的 error 事件
 */
export function useCorsFallback(src: string, onFail?: () => void) {
  const [plainSrc, setPlainSrc] = useState<string | null>(null);
  const cors = plainSrc !== src;
  const onError = useCallback(() => {
    if (cors) setPlainSrc(src);
    else onFail?.();
  }, [cors, src, onFail]);
  return { crossOrigin: cors ? ("anonymous" as const) : null, onError };
}
