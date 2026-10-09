import { create } from "zustand";

import type { GenerateKind } from "@/api/conversation/type";
import { loadPrefs, savePrefs } from "@/utils/conversation/composer-prefs";
import type { ComposerRef } from "@/utils/conversation/submission";

type ComposerState = {
  /** 当前创作模式 */
  mode: GenerateKind;
  /** 每个模式上次选的模型 key；没有记录时输入卡片取清单第一个 */
  modelByMode: Partial<Record<GenerateKind, string>>;
  /** 每个模型的参数取值（含生成方式、生成数量），没填的取模型默认值 */
  paramsByModel: Record<string, Record<string, unknown>>;
  /** 输入框里的草稿 */
  text: string;
  /** 参考图 */
  refs: ComposerRef[];
  /** 外部填充输入框的次数：输入卡片看到它变了，就聚焦、把光标放到末尾并闪一下描边 */
  fillSeq: number;
  /** 是否正在发送 */
  sending: boolean;
  /** 切换创作模式 */
  setMode: (mode: GenerateKind) => void;
  /** 给某个模式选模型 */
  setModel: (mode: GenerateKind, modelKey: string) => void;
  /** 整体替换某个模型的参数 */
  setParams: (modelKey: string, params: Record<string, unknown>) => void;
  /** 改某个模型的一个参数 */
  setParam: (modelKey: string, name: string, value: unknown) => void;
  /** 更新草稿 */
  setText: (text: string) => void;
  /** 从输入框以外的地方（登录页带入、重新编辑）填入草稿 */
  fill: (text: string) => void;
  /** 加一张参考图 */
  addRef: (ref: ComposerRef) => void;
  /** 改一张参考图（上传进度、结果） */
  updateRef: (id: string, patch: Partial<ComposerRef>) => void;
  /** 移除一张参考图 */
  removeRef: (id: string) => void;
  /** 标记是否正在发送 */
  setSending: (sending: boolean) => void;
  /** 发送成功后清空草稿和参考图；模式、模型、参数保留，方便接着做同一类 */
  clearDraft: () => void;
  /** 「重新编辑」：整体换成记录里的内容并触发聚焦 */
  restore: (next: {
    mode: GenerateKind;
    modelId: string;
    params: Record<string, unknown>;
    text: string;
    refs: ComposerRef[];
  }) => void;
};

/** 只留要持久化的部分 */
const prefsOf = (state: ComposerState) => ({
  mode: state.mode,
  modelByMode: state.modelByMode,
  paramsByModel: state.paramsByModel,
});

const initial = loadPrefs();

/**
 * 输入卡片的状态：创作页和对话页各渲染一个输入卡片，
 * 模式、模型、参数、草稿和参考图放在这里，页面之间切换时跟着走，不会丢。
 * 草稿和参考图只在内存里，刷新后重新开始；模式、模型和参数是个人习惯，记在 localStorage。
 */
export const useComposerStore = create<ComposerState>((set) => ({
  mode: initial.mode,
  modelByMode: initial.modelByMode,
  paramsByModel: initial.paramsByModel,
  text: "",
  refs: [],
  fillSeq: 0,
  sending: false,
  setMode: (mode) => set({ mode }),
  setModel: (mode, modelKey) =>
    set((state) => ({ modelByMode: { ...state.modelByMode, [mode]: modelKey } })),
  setParams: (modelKey, params) =>
    set((state) => {
      // 先删再写：保证最近改的排在最后，超出上限时丢的是最久没动的
      const { [modelKey]: _old, ...rest } = state.paramsByModel;
      return { paramsByModel: { ...rest, [modelKey]: params } };
    }),
  setParam: (modelKey, name, value) =>
    set((state) => {
      const { [modelKey]: old, ...rest } = state.paramsByModel;
      return { paramsByModel: { ...rest, [modelKey]: { ...old, [name]: value } } };
    }),
  setText: (text) => set({ text }),
  fill: (text) => set((state) => ({ text, fillSeq: state.fillSeq + 1 })),
  addRef: (ref) => set((state) => ({ refs: [...state.refs, ref] })),
  updateRef: (id, patch) =>
    set((state) => ({
      refs: state.refs.map((ref) => (ref.id === id ? { ...ref, ...patch } : ref)),
    })),
  removeRef: (id) => set((state) => ({ refs: state.refs.filter((ref) => ref.id !== id) })),
  setSending: (sending) => set({ sending }),
  clearDraft: () => set({ text: "", refs: [] }),
  restore: (next) =>
    set((state) => ({
      mode: next.mode,
      modelByMode: { ...state.modelByMode, [next.mode]: next.modelId },
      paramsByModel: { ...state.paramsByModel, [next.modelId]: next.params },
      text: next.text,
      refs: next.refs,
      fillSeq: state.fillSeq + 1,
    })),
}));

/** 偏好变了才写存储：草稿每敲一个字都会触发订阅，不该跟着写 */
let lastPrefs = JSON.stringify(prefsOf(useComposerStore.getState()));
useComposerStore.subscribe((state) => {
  const next = JSON.stringify(prefsOf(state));
  if (next === lastPrefs) return;
  lastPrefs = next;
  savePrefs(prefsOf(state));
});
