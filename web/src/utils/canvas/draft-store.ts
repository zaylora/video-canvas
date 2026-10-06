import type { CanvasGraphDto } from "@/api/canvas/type";

/**
 * 本地草稿：改动先写进浏览器 IndexedDB，再慢慢同步到云端，关页、断网、崩溃都不丢。
 * 草稿存的是和 PUT 的 payload_json 同构的图谱，加上它基于云端哪个 revision。
 * 所有方法都不抛错：IndexedDB 在隐私模式、配额满、被禁用时不可用，调用方降级为只走云端。
 */
export type Draft = {
  schemaVersion: 1;
  graph: CanvasGraphDto;
  /** 这份草稿基于云端哪个 revision */
  baseVersion: number;
  /** true = 还没上传成功 */
  cloudDirty: boolean;
  /** 本地写入时间，用于清理过期草稿 */
  savedAt: number;
};

export type DraftInput = Pick<Draft, "graph" | "baseVersion" | "cloudDirty">;

/** 存储后端：生产环境是 IndexedDB，测试里用内存实现 */
export type DraftBackend = {
  get(key: string): Promise<unknown>;
  put(key: string, value: unknown): Promise<void>;
  delete(key: string): Promise<void>;
  keys(): Promise<string[]>;
};

const keyOf = (userId: string, canvasId: string) => `${userId}:${canvasId}`;

function isDraft(value: unknown): value is Draft {
  if (!value || typeof value !== "object") return false;
  const draft = value as Partial<Draft>;
  return (
    draft.schemaVersion === 1 &&
    typeof draft.baseVersion === "number" &&
    typeof draft.cloudDirty === "boolean" &&
    typeof draft.savedAt === "number" &&
    typeof draft.graph === "object" &&
    draft.graph !== null
  );
}

export function createDraftStore(backend: DraftBackend, now: () => number = Date.now) {
  return {
    /** 写草稿；成功返回 true，失败（配额、权限等）返回 false */
    async save(userId: string, canvasId: string, input: DraftInput) {
      const draft: Draft = { schemaVersion: 1, ...input, savedAt: now() };
      try {
        await backend.put(keyOf(userId, canvasId), draft);
        return true;
      } catch {
        return false;
      }
    },

    /** 读草稿；没有、损坏、版本不认识、读取失败都返回 null */
    async load(userId: string, canvasId: string) {
      try {
        const value = await backend.get(keyOf(userId, canvasId));
        return isDraft(value) ? value : null;
      } catch {
        return null;
      }
    },

    async remove(userId: string, canvasId: string) {
      try {
        await backend.delete(keyOf(userId, canvasId));
      } catch {
        // 删不掉就留着，下次同步成功会再删；不影响保存
      }
    },

    /** 清空这个用户的全部草稿（退出登录用）。前缀带冒号，user 7 不会误删 user 70 */
    async clearUser(userId: string) {
      try {
        const prefix = `${userId}:`;
        const keys = (await backend.keys()).filter((key) => key.startsWith(prefix));
        await Promise.all(keys.map((key) => backend.delete(key)));
      } catch {
        // 清不掉不影响退出
      }
    },

    /** 清理已同步且超过保留期的草稿；还没同步的草稿是用户唯一的副本，永远不清 */
    async purgeExpired(userId: string, maxAgeMs: number) {
      try {
        const prefix = `${userId}:`;
        const keys = (await backend.keys()).filter((key) => key.startsWith(prefix));
        const cutoff = now() - maxAgeMs;
        await Promise.all(
          keys.map(async (key) => {
            const value = await backend.get(key);
            if (isDraft(value) && !value.cloudDirty && value.savedAt < cutoff) {
              await backend.delete(key);
            }
          }),
        );
      } catch {
        // 清理失败下次再来
      }
    },
  };
}
