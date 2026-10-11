import type { NodeMediaType } from "@/types";

import { variantUrl } from "./media-lod";

/** 量一张图的宽 / 高；加载失败或没有尺寸为 null */
function probeImage(url: string): Promise<number | null> {
  return new Promise((resolve) => {
    const image = new Image();
    image.onload = () =>
      resolve(
        image.naturalWidth > 0 && image.naturalHeight > 0
          ? image.naturalWidth / image.naturalHeight
          : null,
      );
    image.onerror = () => resolve(null);
    image.src = url;
  });
}

/** 只读视频元数据量宽 / 高，不下载整段视频；加载失败或没有画面为 null */
function probeVideo(url: string): Promise<number | null> {
  return new Promise((resolve) => {
    const video = document.createElement("video");
    const done = (aspect: number | null) => {
      video.removeAttribute("src");
      video.load();
      resolve(aspect);
    };
    video.preload = "metadata";
    video.onloadedmetadata = () =>
      done(
        video.videoWidth > 0 && video.videoHeight > 0 ? video.videoWidth / video.videoHeight : null,
      );
    video.onerror = () => done(null);
    video.src = url;
  });
}

/**
 * 量素材的真实画幅：图片读缩略图、视频读封面（变体都按长边等比缩放，比例不变），
 * 这样低缩放档也不会为了量尺寸去拉原图；没有变体的地址（外链、blob:）才直接读原图或视频元数据。
 * @param src 节点里的素材地址
 * @param mediaType 素材种类；音频没有画面，直接返回 null
 * @returns 宽 / 高；量不出来为 null
 */
export async function probeMediaAspect(
  src: string,
  mediaType: NodeMediaType,
): Promise<number | null> {
  if (mediaType === "audio") return null;
  const variant = variantUrl(src, mediaType === "image" ? "thumb" : "poster");
  if (variant.hasVariant) {
    const aspect = await probeImage(variant.url);
    if (aspect !== null) return aspect;
  }
  return mediaType === "image" ? probeImage(src) : probeVideo(src);
}
