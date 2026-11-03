import { useEffect } from "react";
import { create } from "zustand";

import type { PageHints } from "@/utils/admin/assistant/page-context";

type PageHintsStore = {
  /** 各后台页面登记的选中项：后台助手发送消息时据此生成 page_context */
  hints: PageHints;
  /** 登记或更新若干键；值为空的键视为清除 */
  patch: (next: PageHints) => void;
  /** 清除若干键（页面卸载时） */
  clear: (keys: (keyof PageHints)[]) => void;
};

/**
 * 后台页面选中项的登记处：模型渠道页登记选中的渠道，模型面板登记打开的模型。
 * 页面和助手抽屉互不引用，只通过这里交换信息
 */
export const useAdminPageHints = create<PageHintsStore>((set) => ({
  hints: {},
  patch: (next) =>
    set((state) => {
      const hints = { ...state.hints };
      for (const key of Object.keys(next) as (keyof PageHints)[]) {
        if (next[key]) hints[key] = next[key];
        else delete hints[key];
      }
      return { hints };
    }),
  clear: (keys) =>
    set((state) => {
      const hints = { ...state.hints };
      for (const key of keys) delete hints[key];
      return { hints };
    }),
}));

/**
 * 在页面里登记选中项：值变化时更新，页面卸载时清除自己登记的键
 * @param hints 这个页面负责的选中项，只写自己负责的键
 */
export function usePageHints(hints: PageHints): void {
  const { modelKey, channelKey, pluginKey } = hints;
  useEffect(() => {
    /** 只动自己登记过的键，不能清掉别的页面登记的 */
    const owned = (Object.keys(hints) as (keyof PageHints)[]).filter((key) => key in hints);
    const next: PageHints = {};
    for (const key of owned) next[key] = hints[key];
    useAdminPageHints.getState().patch(next);
    return () => useAdminPageHints.getState().clear(owned);
    // 键集合在页面生命周期内固定，只有值会变
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modelKey, channelKey, pluginKey]);
}
