import { create } from "zustand";
import { persist } from "zustand/middleware";

import { applyTheme, type ThemeSetting } from "@/lib/theme";

/** 背景网格样式，"none" 表示不画背景 */
export type CanvasBackground = "dots" | "lines" | "cross" | "none";

/** 滚轮的默认行为：平移画布，或直接缩放 */
export type CanvasWheelMode = "pan" | "zoom";

/** 自己接进来的模型：一条就是一个可调用的服务端点 */
export type CustomModel = {
  /** 本地生成的标识，节点数据里存的 model 就是它 */
  id: string;
  /** 显示名，出现在节点的模型下拉里 */
  label: string;
  /** 适用的节点种类 */
  kind: string;
  /** 接口地址 */
  endpoint: string;
  /** 调用凭证，只存在本机浏览器里 */
  apiKey: string;
  /** 服务商那边的模型标识，请求时带的就是它 */
  modelId: string;
  /** 单次消耗的积分 */
  credits: number;
};

/** 存下来的那部分设置，不含下面那些操作 */
export type Settings = {
  // 画布
  background: CanvasBackground;
  /** 拖动节点时吸附到 GRID_SIZE 的整数倍 */
  snapToGrid: boolean;
  wheelMode: CanvasWheelMode;
  // 通用
  theme: ThemeSetting;
  /** 空画布时顶部那条操作提示 */
  showHints: boolean;
  // 模型：种类 -> 模型 id，新建节点时按它预选，没记的种类走清单第一条
  defaultModels: Record<string, string>;
  /** 自定义接入的模型 */
  customModels: CustomModel[];
};

type SettingsActions = {
  updateSettings: <K extends keyof Settings>(key: K, value: Settings[K]) => void;
  /** 单独改某个种类的默认模型，其余种类原样留着 */
  setDefaultModel: (kind: string, modelId: string) => void;
  addCustomModel: (model: Omit<CustomModel, "id">) => void;
  updateCustomModel: (id: string, patch: Omit<CustomModel, "id">) => void;
  removeCustomModel: (id: string) => void;
  resetSettings: () => void;
};

export type SettingsStore = Settings & SettingsActions;

/** 吸附网格的步长，跟背景点阵的默认间距对齐 */
export const GRID_SIZE = 20;

const DEFAULT_SETTINGS: Settings = {
  background: "dots",
  snapToGrid: false,
  wheelMode: "pan",
  theme: "system",
  showHints: true,
  defaultModels: {},
  customModels: [],
};

const STORAGE_KEY = "video-design-settings";

/**
 * 全局设置：画布要读，节点的模型下拉也要读，所以放一个 store 里。
 * persist 负责写进 localStorage，刷新后还在；存档坏了 zustand 会退回默认值。
 */
export const useSettingsStore = create<SettingsStore>()(
  persist(
    (set, get) => ({
      ...DEFAULT_SETTINGS,

      updateSettings: (key, value) => {
        set({ [key]: value } as Pick<Settings, typeof key>);
        if (key === "theme") applyTheme(get().theme);
      },

      setDefaultModel: (kind, modelId) =>
        set({ defaultModels: { ...get().defaultModels, [kind]: modelId } }),

      addCustomModel: (model) =>
        set({
          customModels: [...get().customModels, { ...model, id: `custom-${crypto.randomUUID()}` }],
        }),

      updateCustomModel: (id, patch) =>
        set({
          customModels: get().customModels.map((model) =>
            model.id === id ? { ...patch, id } : model,
          ),
        }),

      // 删模型时顺手把指着它的默认模型清掉，免得默认值指向不存在的模型
      removeCustomModel: (id) =>
        set({
          defaultModels: Object.fromEntries(
            Object.entries(get().defaultModels).filter(([, value]) => value !== id),
          ),
          customModels: get().customModels.filter((model) => model.id !== id),
        }),

      resetSettings: () => {
        set(DEFAULT_SETTINGS);
        applyTheme(DEFAULT_SETTINGS.theme);
      },
    }),
    {
      name: STORAGE_KEY,
      // 只存数据，操作函数不进存档
      partialize: ({
        background,
        snapToGrid,
        wheelMode,
        theme,
        showHints,
        defaultModels,
        customModels,
      }): Settings => ({
        background,
        snapToGrid,
        wheelMode,
        theme,
        showHints,
        defaultModels,
        customModels,
      }),
      // 读回存档后立刻贴上主题，跟着 initTheme 一起保证首屏不闪色
      onRehydrateStorage: () => (state) => {
        if (state) applyTheme(state.theme);
      },
    },
  ),
);

/**
 * 首屏套用存档里的主题。
 * 要在 React 挂载前调用，否则深色存档会先闪一下浅色。
 */
export function initTheme() {
  applyTheme(useSettingsStore.getState().theme);
}
