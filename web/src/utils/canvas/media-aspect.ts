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

/**
 * 只读视频元数据量宽 / 高，不下载整段视频；加载失败或没有画面为 null。
 * 和画布播放、截帧一样先带跨域头（共用浏览器缓存），失败了去掉再试一次，见 useCorsFallback。
 */
function probeVideo(url: string, cors = true): Promise<number | null> {
  return new Promise((resolve) => {
    const video = document.createElement("video");
    const done = (aspect: number | null) => {
      video.removeAttribute("src");
      video.load();
      resolve(aspect);
    };
    video.preload = "metadata";
    if (cors) video.crossOrigin = "anonymous";
    video.onloadedmetadata = () =>
      done(
        video.videoWidth > 0 && video.videoHeight > 0 ? video.videoWidth / video.videoHeight : null,
      );
    video.onerror = () => {
      video.removeAttribute("src");
      video.load();
      resolve(cors ? probeVideo(url, false) : null);
    };
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

/** 本地文件解出来的像素尺寸 */
type PixelSize = { width: number; height: number };

/** 读本地文件尺寸的两个办法，测试时注入假的 */
type FileSizeReaders = {
  imageSize: (file: File) => Promise<PixelSize | null>;
  videoSize: (file: File) => Promise<PixelSize | null>;
};

/** 读本地文件尺寸最多等多久（毫秒）：超时就放弃，不拖住上传 */
const FILE_PROBE_TIMEOUT_MS = 3000;

/** 用 blob 地址临时托着文件，跑完 read 就释放 */
async function withObjectUrl<T>(file: File, read: (url: string) => Promise<T>): Promise<T> {
  const url = URL.createObjectURL(file);
  try {
    return await read(url);
  } finally {
    URL.revokeObjectURL(url);
  }
}

const browserReaders: FileSizeReaders = {
  imageSize: (file) =>
    withObjectUrl(
      file,
      (url) =>
        new Promise((resolve) => {
          const image = new Image();
          image.onload = () => resolve({ width: image.naturalWidth, height: image.naturalHeight });
          image.onerror = () => resolve(null);
          image.src = url;
        }),
    ),
  videoSize: (file) =>
    withObjectUrl(
      file,
      (url) =>
        new Promise((resolve) => {
          const video = document.createElement("video");
          const done = (size: PixelSize | null) => {
            video.removeAttribute("src");
            video.load();
            resolve(size);
          };
          video.preload = "metadata";
          video.onloadedmetadata = () =>
            done({ width: video.videoWidth, height: video.videoHeight });
          video.onerror = () => done(null);
          video.src = url;
        }),
    ),
};

/**
 * 上传前读本地文件的宽高比，让节点一建出来就按真实画幅占位。只读元数据，不上传、不等网络。
 * @param file 要上传的文件
 * @param mediaType 文件种类；音频没有画面，直接返回 null
 * @param readers 读尺寸的办法，默认走浏览器；测试时注入
 * @param timeoutMs 最多等多久，超时返回 null
 * @returns 宽 / 高；读不出来、尺寸为 0、读取出错或超时为 null
 */
export async function probeFileAspect(
  file: File,
  mediaType: NodeMediaType,
  readers: FileSizeReaders = browserReaders,
  timeoutMs: number = FILE_PROBE_TIMEOUT_MS,
): Promise<number | null> {
  if (mediaType === "audio") return null;
  const read = mediaType === "image" ? readers.imageSize : readers.videoSize;
  const timeout = new Promise<null>((resolve) => setTimeout(() => resolve(null), timeoutMs));
  try {
    const size = await Promise.race([read(file), timeout]);
    return size && size.width > 0 && size.height > 0 ? size.width / size.height : null;
  } catch {
    return null;
  }
}

/** 记下的画幅和量到的差在这以内就算一样（比例的相对误差） */
const ASPECT_EPSILON = 0.005;

/**
 * 量到成片画幅后，要不要把它回写进节点的 mediaAspect。
 * @param recorded 节点里已记下的画幅；没记过为 undefined
 * @param measured 量到的成片宽 / 高
 * @returns 没记过或对不上为 true；差在浮点误差内为 false，免得白白触发画布保存
 */
export function shouldRecordAspect(recorded: number | undefined, measured: number): boolean {
  if (recorded === undefined) return true;
  return Math.abs(recorded - measured) / measured > ASPECT_EPSILON;
}
