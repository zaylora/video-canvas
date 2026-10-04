/**
 * 画布素材加载提速的纯逻辑（设计稿 docs/design/画布素材加载设计 第 6.3、6.4 节）：
 * 变体地址、缩放分档的滞回、视频并发名额。不碰 React，方便单测。
 */

/** 素材变体：thumb 是图片缩略图，poster 是视频封面 */
export type MediaVariant = "thumb" | "poster";

/** 变体地址的解析结果 */
export type VariantUrl = {
  /** 可以直接塞给 img 的地址；无变体时就是原地址 */
  url: string;
  /** false 表示地址不归后端 /files/ 管（外链、blob:、data:），没有变体可用 */
  hasVariant: boolean;
};

/** 匹配「可选的 http(s) 域名 + /files/ 路径」 */
const FILES_URL = /^(?:https?:\/\/[^/?#]+)?\/files\//i;

/**
 * 给后端 /files/ 地址追加 `v=thumb|poster`，其余 query 与 hash 原样保留；已有的 v 会被替换。
 * 为什么不改 src 本身：payload 里的稳定地址不能变，变体只在展示时拼出来。
 * @param src 节点里的素材地址，相对或绝对均可
 * @param variant 要的变体
 * @returns 变体地址；非 /files/ 地址原样返回并标记无变体
 */
export function variantUrl(src: string, variant: MediaVariant): VariantUrl {
  if (!FILES_URL.test(src)) return { url: src, hasVariant: false };
  const hashAt = src.indexOf("#");
  const hash = hashAt >= 0 ? src.slice(hashAt) : "";
  const rest = hashAt >= 0 ? src.slice(0, hashAt) : src;
  const queryAt = rest.indexOf("?");
  const path = queryAt >= 0 ? rest.slice(0, queryAt) : rest;
  const kept = (queryAt >= 0 ? rest.slice(queryAt + 1) : "")
    .split("&")
    .filter((pair) => pair !== "" && pair.split("=")[0] !== "v");
  kept.push(`v=${variant}`);
  return { url: `${path}?${kept.join("&")}${hash}`, hasVariant: true };
}

/** 缩放低于它（降级）才切到缩略图 */
export const LOD_DOWN_ZOOM = 0.55;
/** 缩放回到它以上（升级）才切回原图；和降级阈值之间是滞回带，防止在 0.6 附近来回抖动 */
export const LOD_UP_ZOOM = 0.65;

/**
 * 缩放分档的滞回状态机：只有越过滞回带的另一侧才翻转。
 * @param low 当前是否处于低清（缩略图）档
 * @param zoom 画布当前缩放
 * @returns 新的低清档状态
 */
export function nextLowDetail(low: boolean, zoom: number): boolean {
  if (low) return zoom < LOD_UP_ZOOM;
  return zoom < LOD_DOWN_ZOOM;
}

/** 同时挂载的 video 上限 */
export const MAX_MOUNTED_VIDEOS = 4;

/** 占着视频名额的一方留给名额池的把手 */
export type VideoSlotHandle = {
  /** 此刻是否正在播放；暂停、未开播、播完都算 false */
  isPlaying: () => boolean;
  /** 池子要回名额：调用方应把视频退回封面并释放媒体资源 */
  release: () => void;
};

/** 视频名额池 */
export type VideoSlotPool = {
  /** 登记一个新挂载的视频，必要时先回收已暂停的 */
  acquire: (id: string, handle: VideoSlotHandle) => void;
  /** 归还名额（卸载或退回封面时调用），重复调用无副作用 */
  release: (id: string) => void;
  /** 当前占用的名额数 */
  size: () => number;
};

/**
 * 创建视频并发名额池。
 * 满额时先把已暂停的退回封面；正在播放的绝不打断。
 * 如果全都在播放，则放行超限：用户刚点的播放不能被拒绝，上限是软限制，
 * 等任何一个暂停后，下一次点击时会被回收。
 * @param limit 名额上限
 * @returns 名额池
 */
export function createVideoSlotPool(limit: number = MAX_MOUNTED_VIDEOS): VideoSlotPool {
  const slots = new Map<string, VideoSlotHandle>();
  return {
    acquire(id, handle) {
      if (!slots.has(id)) {
        for (const [otherId, other] of [...slots]) {
          if (slots.size < limit) break;
          if (other.isPlaying()) continue;
          slots.delete(otherId);
          other.release();
        }
      }
      slots.set(id, handle);
    },
    release(id) {
      slots.delete(id);
    },
    size: () => slots.size,
  };
}

/**
 * 毫秒时长转成角标文字。
 * @param ms 时长，毫秒
 * @returns `m:ss`，超过一小时为 `h:mm:ss`；没有有效时长返回 null
 */
export function formatMediaDuration(ms: number | null | undefined): string | null {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return null;
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}
