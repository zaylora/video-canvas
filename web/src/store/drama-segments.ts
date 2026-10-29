import { create } from "zustand";

import type { Segment } from "@/api/drama/type";
import { mergeSegment, removeSegmentFrom, sortSegments } from "@/utils/drama/segments";

/** 一部短剧的一集：片段列表的键 */
export const segmentKey = (dramaId: string, no: number) => `${dramaId}:${no}`;

/** 解开 segmentKey */
export function parseSegmentKey(key: string): { dramaId: string; no: number } {
  const at = key.lastIndexOf(":");
  return { dramaId: key.slice(0, at), no: Number(key.slice(at + 1)) };
}

type DramaSegmentsState = {
  /** 每集的片段，按 seq 排序；没加载过的集没有键 */
  byEpisode: Record<string, Segment[]>;
  /** 正在打开的片段工作区（引用计数）：WebSocket 重连 / 回到前台时只对账这些 */
  watching: Record<string, number>;
  /** 整体替换一集的片段（HTTP 读取 / 重排返回） */
  setList: (dramaId: string, no: number, list: readonly Segment[]) => void;
  /** 并入一条片段快照（WebSocket 推送 / 接口返回） */
  apply: (dramaId: string, no: number, segment: Segment) => void;
  /** 删掉一个片段 */
  remove: (dramaId: string, no: number, id: string) => void;
  /** 片段工作区挂载时登记，卸载时取消 */
  watch: (dramaId: string, no: number) => () => void;
  /** 丢掉一部短剧缓存的全部集（删集后集号补位，按集号存的旧缓存会对错集），下次打开重新读 */
  forgetDrama: (dramaId: string) => void;
};

export const useDramaSegmentsStore = create<DramaSegmentsState>((set) => ({
  byEpisode: {},
  watching: {},

  setList: (dramaId, no, list) =>
    set((state) => ({
      byEpisode: { ...state.byEpisode, [segmentKey(dramaId, no)]: sortSegments(list) },
    })),

  apply: (dramaId, no, segment) =>
    set((state) => {
      const key = segmentKey(dramaId, no);
      return {
        byEpisode: { ...state.byEpisode, [key]: mergeSegment(state.byEpisode[key] ?? [], segment) },
      };
    }),

  remove: (dramaId, no, id) =>
    set((state) => {
      const key = segmentKey(dramaId, no);
      return {
        byEpisode: { ...state.byEpisode, [key]: removeSegmentFrom(state.byEpisode[key] ?? [], id) },
      };
    }),

  watch: (dramaId, no) => {
    const key = segmentKey(dramaId, no);
    set((state) => ({ watching: { ...state.watching, [key]: (state.watching[key] ?? 0) + 1 } }));
    return () =>
      set((state) => {
        const count = (state.watching[key] ?? 1) - 1;
        const watching = { ...state.watching };
        if (count <= 0) delete watching[key];
        else watching[key] = count;
        return { watching };
      });
  },

  forgetDrama: (dramaId) =>
    set((state) => ({
      byEpisode: Object.fromEntries(
        Object.entries(state.byEpisode).filter(([key]) => parseSegmentKey(key).dramaId !== dramaId),
      ),
    })),
}));
