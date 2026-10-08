import { create } from "zustand";

import { deleteAvatar, getMe, patchMe, uploadAvatar } from "@/api/me";
import type { MeDto } from "@/api/me/type";

/** 当前用户资料的加载状态 */
export type MeStatus = "idle" | "loading" | "ready" | "error";

/** store 依赖的接口，测试里可替换 */
type MeApi = {
  /** GET /me */
  getMe: typeof getMe;
  /** PATCH /me */
  patchMe: typeof patchMe;
  /** POST /me/avatar */
  uploadAvatar: typeof uploadAvatar;
  /** DELETE /me/avatar */
  deleteAvatar: typeof deleteAvatar;
};

type MeState = {
  /** 当前用户资料；未加载时为 null */
  me: MeDto | null;
  /** 加载状态；失败时保留上一次成功的 me */
  status: MeStatus;
  /** 拉最新资料：登录后进入应用和启动时各拉一次；同一时刻多处触发只发一次请求 */
  fetchMe: () => Promise<void>;
  /** 用接口返回的新资料替换，侧栏、画布右上角、身份卡同步刷新 */
  setMe: (me: MeDto) => void;
  /** 改昵称；失败抛 ApiError，全局 toast 已由拦截器弹出 */
  updateNickname: (nickname: string) => Promise<void>;
  /** 上传裁剪好的头像 */
  updateAvatar: (file: Blob, fileName: string) => Promise<void>;
  /** 移除头像，恢复首字母头像 */
  removeAvatar: () => Promise<void>;
};

/**
 * 创建当前用户 store。改密码不需要重新拉取：资料没变，令牌由 setToken 续上
 * @param overrides 替换部分接口（测试用）
 */
export function createMeStore(overrides: Partial<MeApi> = {}) {
  const api: MeApi = { getMe, patchMe, uploadAvatar, deleteAvatar, ...overrides };
  let inflight: Promise<void> | null = null;

  return create<MeState>((set) => ({
    me: null,
    status: "idle",
    fetchMe: () => {
      inflight ??= (async () => {
        set((state) => ({ status: state.me ? state.status : "loading" }));
        try {
          set({ me: await api.getMe(), status: "ready" });
        } catch {
          set({ status: "error" });
        } finally {
          inflight = null;
        }
      })();
      return inflight;
    },
    setMe: (me) => set({ me, status: "ready" }),
    updateNickname: async (nickname) => {
      set({ me: await api.patchMe({ nickname }), status: "ready" });
    },
    updateAvatar: async (file, fileName) => {
      set({ me: await api.uploadAvatar(file, fileName), status: "ready" });
    },
    removeAvatar: async () => {
      set({ me: await api.deleteAvatar(), status: "ready" });
    },
  }));
}

/** 全站共用的当前用户 store */
export const useMeStore = createMeStore();
