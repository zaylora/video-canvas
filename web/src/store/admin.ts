import { create } from "zustand";
import { persist } from "zustand/middleware";

import { getAdminMe } from "@/api/admin/ai";
import type { AdminRole } from "@/api/admin/ai/type";
import { isForbiddenError, normalizeRole } from "@/utils/admin/role";
import { saveRole } from "@/utils/storage/token";

/** 角色加载状态 */
export type AdminStatus =
  /** 还没加载 */
  | "idle"
  /** 加载中 */
  | "loading"
  /** 已确认是管理员（role 有值） */
  | "ready"
  /** 后端返回 403：不是管理员 */
  | "forbidden"
  /** 加载失败（网络等，不等于没有权限），可重试 */
  | "error";

/** 管理端角色 store：布局进入时加载一次，页面只读这里，不重复请求 */
export type AdminStore = {
  /** 当前管理端角色；未确认（idle / loading / forbidden / error）时为 null */
  role: AdminRole | null;
  /** 当前管理员自己的用户 ID；用户管理页据此禁用「对自己的操作」，未确认时为 null */
  userId: number | null;
  /** 加载状态 */
  status: AdminStatus;
  /** 后台助手抽屉是否展开：不持久化，每次进后台默认收起 */
  assistantOpen: boolean;
  /**
   * 调用 GET /admin/ai/me 取角色。已在加载中时忽略；ready 后再调用不重新请求，除非 force。
   * 403 记为 forbidden，其余错误记为 error，请求错误的全局提示由拦截器负责，这里不重复弹。
   * @param force 强制重新请求（403 页的“重试”按钮）
   */
  load: (force?: boolean) => Promise<void>;
  /** 清空角色（退出登录等场景） */
  reset: () => void;
  /** 展开或收起后台助手抽屉 */
  setAssistantOpen: (open: boolean) => void;
  /** 切换后台助手抽屉 */
  toggleAssistant: () => void;
};

/**
 * 后台 store 的本地持久化规则：角色和加载状态每次进后台都要重新确认；助手抽屉也不记，
 * 每次进后台默认收起。目前没有需要跨会话记住的界面偏好，所以不存任何字段；
 * 旧存档里存过 assistantOpen: true，读回时丢掉，避免升级后第一次还是展开。
 */
export const adminPersistRules = {
  partialize: (): Record<string, never> => ({}),
  merge: (_persisted: unknown, current: AdminStore): AdminStore => current,
};

export const useAdminStore = create<AdminStore>()(
  persist(
    (set, get) => ({
      role: null,
      userId: null,
      status: "idle",
      assistantOpen: false,
      load: async (force = false) => {
        const { status } = get();
        if (status === "loading" || (status === "ready" && !force)) return;
        set({ status: "loading" });
        try {
          const me = await getAdminMe();
          const role = normalizeRole(me?.role);
          saveRole(role);
          set({
            role,
            userId: typeof me?.user_id === "number" ? me.user_id : null,
            status: "ready",
          });
        } catch (error) {
          set({
            role: null,
            userId: null,
            status: isForbiddenError(error) ? "forbidden" : "error",
          });
        }
      },
      reset: () => set({ role: null, userId: null, status: "idle" }),
      setAssistantOpen: (open) => set({ assistantOpen: open }),
      toggleAssistant: () => set((state) => ({ assistantOpen: !state.assistantOpen })),
    }),
    {
      name: "video-canvas-admin-ui",
      ...adminPersistRules,
    },
  ),
);
