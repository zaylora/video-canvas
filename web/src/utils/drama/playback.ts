import type { Segment } from "@/api/drama/type";

import { clipWidth, playableTake, sortSegments } from "./segments";

/** 片段条上相邻两格的间距（像素），和 clip-bar 里 Reorder.Group 的 gap-1 一致 */
export const CLIP_GAP = 4;

/** 连播列表里的一项：一个有采用视频的片段 */
export type PlayItem = {
  /** 片段 id */
  id: string;
  seq: number;
  /** 视频地址 */
  url: string;
  /** 这一段的时长（秒） */
  seconds: number;
  /** 在整条连播时间轴上的起止（秒） */
  start: number;
  end: number;
};

/**
 * 连播列表：按 seq 排序，没有采用视频的片段跳过。
 * 时长优先用素材的实际时长，没有再用片段的计划秒数；时间轴首尾相接。
 */
export function buildPlaylist(segments: readonly Segment[]): PlayItem[] {
  const items: PlayItem[] = [];
  let cursor = 0;
  for (const segment of sortSegments(segments)) {
    const take = playableTake(segment);
    if (!take?.asset) continue;
    const real = take.asset.durationMs ? take.asset.durationMs / 1000 : 0;
    const seconds = real > 0 ? real : segment.seconds;
    items.push({
      id: segment.id,
      seq: segment.seq,
      url: take.asset.url,
      seconds,
      start: cursor,
      end: cursor + seconds,
    });
    cursor += seconds;
  }
  return items;
}

/** 连播总时长（秒） */
export const playlistTotal = (playlist: readonly PlayItem[]): number => playlist.at(-1)?.end ?? 0;

/** 时间轴上的某一刻落在哪一段、段内偏移多少；越界取两端，空列表为 null */
export function locate(
  playlist: readonly PlayItem[],
  time: number,
): { index: number; offset: number } | null {
  if (playlist.length === 0) return null;
  const last = playlist.length - 1;
  if (time <= 0) return { index: 0, offset: 0 };
  if (time >= playlist[last].end) {
    return { index: last, offset: playlist[last].end - playlist[last].start };
  }
  const index = playlist.findIndex((item) => time < item.end);
  return { index, offset: time - playlist[index].start };
}

/** `locate` 的反函数：第 index 段、段内偏移 offset 秒 = 时间轴上的哪一刻；偏移不超过该段时长 */
export function timeAt(playlist: readonly PlayItem[], index: number, offset: number): number {
  const item = playlist[index];
  if (!item) return 0;
  return item.start + Math.min(Math.max(offset, 0), item.end - item.start);
}

/** 下一段的下标；已经是最后一段为 null */
export const nextIndex = (playlist: readonly PlayItem[], index: number): number | null =>
  index + 1 < playlist.length ? index + 1 : null;

/** 从某个片段开始（含）第一个能播的连播项的下标；它之后没有能播的为 -1 */
export const firstIndexFrom = (playlist: readonly PlayItem[], segment: Pick<Segment, "seq">) =>
  playlist.findIndex((item) => item.seq >= segment.seq);

/** 片段条上每个片段的起点（像素）：前面各片段的宽度加间距，没有视频的片段也占位 */
export function clipOffsets(segments: readonly Pick<Segment, "seconds">[]): number[] {
  const offsets: number[] = [];
  let x = 0;
  for (const segment of segments) {
    offsets.push(x);
    x += clipWidth(segment.seconds) + CLIP_GAP;
  }
  return offsets;
}

/** 播放头在片段条上的位置：片段起点 + 进度（0–1）× 该片段的宽度；找不到片段为 0 */
export function playheadX(
  segments: readonly Pick<Segment, "id" | "seconds">[],
  segmentId: string,
  progress: number,
): number {
  const index = segments.findIndex((segment) => segment.id === segmentId);
  if (index < 0) return 0;
  const ratio = Math.min(Math.max(progress, 0), 1);
  return clipOffsets(segments)[index] + clipWidth(segments[index].seconds) * ratio;
}
