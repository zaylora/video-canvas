import { create } from "zustand";

import { toast } from "sonner";

import { deleteConversationRecord, getConversationRecords } from "@/api/conversation";
import type {
  RecordDto,
  SubmitRecordRequest,
  SubmitRecordResultDto,
  SubmitTarget,
} from "@/api/conversation/type";
import type { TaskView } from "@/api/generation-task/type";
import { useConversationsStore } from "@/store/conversations";
import { useTasksStore } from "@/store/tasks";
import { appendRecord, mergeOlderPage, removeRecord } from "@/utils/conversation/records";
import { submitRecordTasks } from "@/utils/tasks/gateway";
import { describeSubmitError } from "@/utils/tasks/submit";

/** 一段对话的记录 */
export type RecordsEntry = {
  /** 记录，时间正序（最新的在最后） */
  items: RecordDto[];
  /** 更早一页的游标；null 表示已经到头 */
  next: string | null;
  /** 首屏加载状态 */
  status: "loading" | "ready" | "error";
  /** 是否正在加载更早的记录 */
  loadingMore: boolean;
};

type RecordsState = {
  /** 对话 ID -> 记录 */
  byId: Record<string, RecordsEntry>;
  /** 加载最新一页：已有缓存时先用缓存，拿到后整体替换（翻过的旧页需要时再翻） */
  load: (conversationId: string) => Promise<void>;
  /** 加载更早的一页 */
  loadMore: (conversationId: string) => Promise<void>;
  /**
   * 提交一条生成记录并放进缓存：同一次点击的重试复用同一个 Idempotency-Key；
   * 成功后刷新余额和侧栏对话；某一格提交失败（积分不足、并发已满）时记录照常创建，用 toast 说明原因。
   * 请求本身失败会抛错，错误提示由请求层统一弹。
   */
  submit: (target: SubmitTarget, body: SubmitRecordRequest) => Promise<SubmitRecordResultDto>;
  /** 追加一条新记录（发送成功后）；这段对话还没加载过时什么都不做，打开页面时会整体加载 */
  upsert: (conversationId: string, record: RecordDto) => void;
  /** 删除一条记录；调用后端成功后才从列表去掉 */
  remove: (conversationId: string, recordId: string, cancelActive: boolean) => Promise<void>;
  /** 忘掉一段对话的缓存（对话被删除后） */
  drop: (conversationId: string) => void;
};

/** 每页条数；与后端默认值一致 */
const PAGE_SIZE = 20;

/** 记录里带着的任务快照写进任务库：之后格子订阅任务库，WS 推送和对账会继续更新它们 */
export const seedTasks = (records: RecordDto[]) => {
  const views = records.flatMap((record) =>
    record.tasks.filter((task): task is TaskView => task !== null),
  );
  if (views.length > 0) useTasksStore.getState().upsertMany(views);
};

/**
 * 对话的记录缓存。记录只带「提交了什么」和任务快照，任务状态以任务库（store/tasks.ts）为准。
 */
export const useConversationRecordsStore = create<RecordsState>((set, get) => {
  const patch = (id: string, change: Partial<RecordsEntry>) =>
    set((state) => {
      const entry = state.byId[id];
      return entry ? { byId: { ...state.byId, [id]: { ...entry, ...change } } } : state;
    });

  return {
    byId: {},

    load: async (conversationId) => {
      const cached = get().byId[conversationId];
      if (!cached) {
        set((state) => ({
          byId: {
            ...state.byId,
            [conversationId]: { items: [], next: null, status: "loading", loadingMore: false },
          },
        }));
      }
      try {
        const page = await getConversationRecords(conversationId, { limit: PAGE_SIZE });
        seedTasks(page.items);
        set((state) => ({
          byId: {
            ...state.byId,
            [conversationId]: {
              items: [...page.items].reverse(),
              next: page.next,
              status: "ready",
              loadingMore: false,
            },
          },
        }));
      } catch {
        // 错误提示由请求层统一弹；有缓存就继续用缓存
        if (!cached) patch(conversationId, { status: "error" });
      }
    },

    loadMore: async (conversationId) => {
      const entry = get().byId[conversationId];
      if (!entry || !entry.next || entry.loadingMore) return;
      patch(conversationId, { loadingMore: true });
      try {
        const page = await getConversationRecords(conversationId, {
          before: entry.next,
          limit: PAGE_SIZE,
        });
        seedTasks(page.items);
        const latest = get().byId[conversationId];
        if (!latest) return;
        patch(conversationId, {
          items: mergeOlderPage(latest.items, page.items),
          next: page.next,
          loadingMore: false,
        });
      } catch {
        patch(conversationId, { loadingMore: false });
      }
    },

    submit: async (target, body) => {
      // 幂等键、重试、任务快照入库、刷新余额都在统一入口里（utils/tasks/gateway.ts）
      const result = await submitRecordTasks(target, body);
      get().upsert(result.conversationId, result.record);
      void useConversationsStore.getState().load();
      const { submitErrors, count } = result.record;
      if (submitErrors.length > 0) {
        const info = describeSubmitError({
          code: submitErrors[0].code,
          status: submitErrors[0].status,
        });
        toast.error(
          submitErrors.length === count
            ? info.message
            : `有 ${submitErrors.length} 个没有提交成功：${info.message}`,
        );
      }
      return result;
    },

    upsert: (conversationId, record) => {
      const entry = get().byId[conversationId];
      if (!entry) return;
      patch(conversationId, { items: appendRecord(entry.items, record) });
    },

    remove: async (conversationId, recordId, cancelActive) => {
      await deleteConversationRecord(conversationId, recordId, cancelActive);
      const entry = get().byId[conversationId];
      if (entry) patch(conversationId, { items: removeRecord(entry.items, recordId) });
    },

    drop: (conversationId) =>
      set((state) => {
        const { [conversationId]: _removed, ...rest } = state.byId;
        return { byId: rest };
      }),
  };
});
