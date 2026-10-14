import { create } from "zustand";

import { checkSystemUpdate } from "@/api/admin/system";
import type { UpdateCheck } from "@/api/admin/system/type.d";

/** 新版本检查结果 store：侧栏版本标记和「版本与升级」页共用，进后台只请求一次（后端还有 1 小时缓存） */
export type SystemUpdateStore = {
  /** 最近一次检查结果；还没检查或请求失败为 null */
  check: UpdateCheck | null;
  /** 正在请求 */
  checking: boolean;
  /** 一键升级正在进行：侧栏版本标记显示转圈、藏起新版本圆点 */
  upgrading: boolean;
  /**
   * 检查新版本。已有结果且不强制时不重复请求；请求中再调用直接忽略。
   * @param force 「重新检查」按钮：跳过后端缓存
   */
  load: (force?: boolean) => Promise<void>;
  /**
   * 标记一键升级是否进行中
   * @param upgrading 是否进行中
   */
  setUpgrading: (upgrading: boolean) => void;
};

export const useSystemUpdateStore = create<SystemUpdateStore>((set, get) => ({
  check: null,
  checking: false,
  upgrading: false,
  load: async (force = false) => {
    const { check, checking } = get();
    if (checking || (check && !force)) return;
    set({ checking: true });
    try {
      set({ check: await checkSystemUpdate(force) });
    } catch {
      /** 全局 toast 已弹；保留上一次的结果 */
    } finally {
      set({ checking: false });
    }
  },
  setUpgrading: (upgrading) => set({ upgrading }),
}));
