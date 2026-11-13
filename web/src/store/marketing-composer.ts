import { create } from "zustand";

import type { ComposerDraft } from "@/utils/marketing/composer";

/** 生成框里的一份上传素材 */
export type MarketingSlot = {
  /** 本地唯一标识：上传期间还没有素材 ID 时用它区分 */
  id: string;
  /** 素材 ID（十进制数字串），上传完成后才有 */
  assetId?: string;
  /** 预览地址：上传前是本地 blob，上传后是素材地址 */
  url?: string;
  /** 文件名 */
  name: string;
  /** 上传状态 */
  status: "uploading" | "done" | "error";
  /** 参考视频的时长（秒），上传完成后才知道 */
  durationSec?: number;
  /** 文件体积（字节） */
  sizeBytes?: number;
};

/** 默认设置：抖音、9:16、720p、15 秒、中文，角度自动 ×2 */
export const DEFAULT_DRAFT: ComposerDraft = {
  platform: "douyin",
  language: "zh",
  angles: [],
  autoCount: 2,
  adaptCount: 1,
  aspectRatio: "9:16",
  resolution: "720p",
  duration: 15,
  textModel: "",
  description: "",
};

type ComposerState = {
  /** 商品图（1–4 张） */
  products: MarketingSlot[];
  /** 模特图（0–1 张） */
  model: MarketingSlot | null;
  /** 参考视频（0–1 段） */
  ref: MarketingSlot | null;
  /** 设置与描述 */
  draft: ComposerDraft;
  /** 选的视频模型 key；空串 = 取第一个可用的 */
  videoModel: string;
  /** 正在提交 */
  sending: boolean;
  /** 改设置 */
  setDraft: (patch: Partial<ComposerDraft>) => void;
  /** 选视频模型 */
  setVideoModel: (key: string) => void;
  /** 加一张商品图 */
  addProduct: (slot: MarketingSlot) => void;
  /** 改一张商品图（上传进度、结果） */
  updateProduct: (id: string, patch: Partial<MarketingSlot>) => void;
  /** 移除一张商品图 */
  removeProduct: (id: string) => void;
  /** 设置 / 清空模特图 */
  setModel: (slot: MarketingSlot | null) => void;
  /** 改模特图 */
  updateModel: (id: string, patch: Partial<MarketingSlot>) => void;
  /** 设置 / 清空参考视频 */
  setRef: (slot: MarketingSlot | null) => void;
  /** 改参考视频 */
  updateRef: (id: string, patch: Partial<MarketingSlot>) => void;
  /** 标记是否正在提交 */
  setSending: (sending: boolean) => void;
  /** 新建项目后清空素材和描述；平台、模型、参数保留，方便接着做 */
  clearAfterProject: () => void;
  /** 在详情页新开一轮后：清描述和参考视频，模特图和商品图保留 */
  clearAfterRound: () => void;
};

const patchSlot = (slot: MarketingSlot | null, id: string, patch: Partial<MarketingSlot>) =>
  slot && slot.id === id ? { ...slot, ...patch } : slot;

/**
 * 营销生成框的状态：列表页的大卡片和详情页底部的 dock 共用一份，页面切换时设置、模型和参数跟着走。
 * 只在内存里，刷新后重新开始。
 */
export const useMarketingComposerStore = create<ComposerState>((set) => ({
  products: [],
  model: null,
  ref: null,
  draft: DEFAULT_DRAFT,
  videoModel: "",
  sending: false,
  setDraft: (patch) => set((state) => ({ draft: { ...state.draft, ...patch } })),
  setVideoModel: (key) => set({ videoModel: key }),
  addProduct: (slot) => set((state) => ({ products: [...state.products, slot] })),
  updateProduct: (id, patch) =>
    set((state) => ({
      products: state.products.map((item) => (item.id === id ? { ...item, ...patch } : item)),
    })),
  removeProduct: (id) =>
    set((state) => ({ products: state.products.filter((item) => item.id !== id) })),
  setModel: (slot) => set({ model: slot }),
  updateModel: (id, patch) => set((state) => ({ model: patchSlot(state.model, id, patch) })),
  setRef: (slot) => set({ ref: slot }),
  updateRef: (id, patch) => set((state) => ({ ref: patchSlot(state.ref, id, patch) })),
  setSending: (sending) => set({ sending }),
  clearAfterProject: () =>
    set((state) => ({
      products: [],
      model: null,
      ref: null,
      draft: { ...state.draft, description: "" },
    })),
  clearAfterRound: () =>
    set((state) => ({ ref: null, draft: { ...state.draft, description: "" } })),
}));
