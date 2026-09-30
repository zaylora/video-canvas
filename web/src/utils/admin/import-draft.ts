/**
 * “导入模型”→“模型页”之间用 sessionStorage 传草稿：长 JSON 不放进 URL，
 * URL 里只带一个带时间戳的键。读写都 try/catch（隐私模式下可能抛错），失败时退化为“不预填”。
 */

/** 暂存键前缀 */
export const IMPORT_DRAFT_PREFIX = "admin_ai_import_draft:";

/** 暂存内容 */
export type StashedDrafts = {
  /** 来源渠道 key */
  channelKey: string;
  /** 待处理的模型正文（draftToModelBody 的结果），按用户勾选顺序 */
  bodies: Record<string, unknown>[];
};

/** 默认存储：sessionStorage；不可用时返回 null */
const defaultStorage = (): Pick<Storage, "getItem" | "setItem" | "removeItem"> | null => {
  try {
    return globalThis.sessionStorage ?? null;
  } catch {
    return null;
  }
};

type StorageLike = Pick<Storage, "getItem" | "setItem" | "removeItem">;

/**
 * 暂存草稿，返回放进 URL 的键；存不进去返回 null。
 * @param payload 要带到模型页的内容
 * @param storage 存储（测试注入用），默认 sessionStorage
 * @param now 时间戳（测试注入用）
 */
export function stashDrafts(
  payload: StashedDrafts,
  storage: StorageLike | null = defaultStorage(),
  now: number = Date.now(),
): string | null {
  if (!storage) return null;
  const id = String(now);
  try {
    storage.setItem(IMPORT_DRAFT_PREFIX + id, JSON.stringify(payload));
    return id;
  } catch {
    return null;
  }
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** 读出暂存的草稿；键不存在、内容损坏、存储不可用都返回 null（不预填） */
export function readStashedDrafts(
  id: string | null | undefined,
  storage: StorageLike | null = defaultStorage(),
): StashedDrafts | null {
  if (!id || !storage) return null;
  try {
    const raw = storage.getItem(IMPORT_DRAFT_PREFIX + id);
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed) || !Array.isArray(parsed.bodies)) return null;
    const bodies = parsed.bodies.filter(isRecord);
    if (bodies.length === 0) return null;
    return {
      channelKey: typeof parsed.channelKey === "string" ? parsed.channelKey : "",
      bodies,
    };
  } catch {
    return null;
  }
}

/** 把还没处理的草稿写回（处理完一个就少一个）；剩余为空则删除暂存 */
export function updateStashedDrafts(
  id: string,
  payload: StashedDrafts,
  storage: StorageLike | null = defaultStorage(),
) {
  if (!storage) return;
  try {
    if (payload.bodies.length === 0) storage.removeItem(IMPORT_DRAFT_PREFIX + id);
    else storage.setItem(IMPORT_DRAFT_PREFIX + id, JSON.stringify(payload));
  } catch {
    // 写不进去就算了：本页内存里的队列仍然有效
  }
}
