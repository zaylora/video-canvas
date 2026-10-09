import { create } from "zustand";

import {
  createConversation,
  deleteConversation,
  getConversations,
  renameConversation,
} from "@/api/conversation";
import type { ConversationDto } from "@/api/conversation/type";

type ConversationsState = {
  /** 对话列表：按最近记录时间倒序 */
  items: ConversationDto[];
  /** 加载状态 */
  status: "idle" | "loading" | "ready" | "error";
  /** 加载列表；同一时刻多处触发只发一次请求，失败时保留已有列表 */
  load: () => Promise<void>;
  /** 新建一段对话并加到列表里 */
  create: (title?: string) => Promise<ConversationDto>;
  /** 重命名 */
  rename: (id: string, title: string) => Promise<void>;
  /** 删除；返回前先从列表里去掉 */
  remove: (id: string) => Promise<void>;
};

let inflight: Promise<void> | null = null;

/**
 * 侧栏「对话」列表。列表不大（每人最多 200 段），一次取全，不分页。
 * 对话的记录不在这里，见 store/conversation-records.ts。
 */
export const useConversationsStore = create<ConversationsState>((set, get) => ({
  items: [],
  status: "idle",

  load: () => {
    inflight ??= (async () => {
      // 已有列表时后台刷新不切回 loading，免得侧栏闪一下骨架
      if (get().status !== "ready") set({ status: "loading" });
      try {
        set({ items: await getConversations(), status: "ready" });
      } catch {
        // 错误提示由请求层统一弹；有旧列表就继续用
        set({ status: get().items.length > 0 ? "ready" : "error" });
      } finally {
        inflight = null;
      }
    })();
    return inflight;
  },

  create: async (title) => {
    const created = await createConversation(title);
    set({ items: [...get().items, created] });
    return created;
  },

  rename: async (id, title) => {
    await renameConversation(id, title);
    set({ items: get().items.map((item) => (item.id === id ? { ...item, title } : item)) });
  },

  remove: async (id) => {
    await deleteConversation(id);
    set({ items: get().items.filter((item) => item.id !== id) });
  },
}));
