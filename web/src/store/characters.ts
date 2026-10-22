import { create } from "zustand";

import { CHARACTER_IDS_LIMIT, getCharactersByIds } from "@/api/character";
import type { CharacterDto } from "@/api/character/type";

/** 缓存里一张角色卡的状态 */
export type CharacterEntry = {
  /** loading 首次读取中；ready 已读到；missing 后端查不到（已删除，节点判失效）；error 读取失败 */
  status: "loading" | "ready" | "missing" | "error";
  /** 最近一次读到的内容；error 时保留旧内容供节点占位，missing 与首次 loading 时没有 */
  card?: CharacterDto;
  /** 最近一次读取完成的时间（毫秒时间戳）；0 表示已失效，下次 ensure 会重读 */
  loadedAt: number;
};

/** 缓存多久后 ensure 会在后台重读（资产页的改 / 删会主动写入或失效，这里兜底别处的改动） */
export const CHARACTER_TTL = 5 * 60 * 1000;

type CharactersState = {
  /** 角色卡 ID -> 缓存项，画布节点从这里渲染（实时引用） */
  byId: Record<string, CharacterEntry>;
  /** 保证这些卡在缓存里且新鲜：缺的与过期的合并成批量请求；已有内容的过期项后台重读，不退回 loading */
  ensure: (ids: string[]) => Promise<void>;
  /** 无视缓存强制重读（提交任务前用），返回读完后这些 ID 的最新状态 */
  refresh: (ids: string[]) => Promise<Record<string, CharacterEntry | undefined>>;
  /** 资产页保存后写入最新内容 */
  upsert: (card: CharacterDto) => void;
  /** 资产页删除后标记为查不到，引用它的节点变已失效 */
  remove: (id: string) => void;
  /** 让缓存里的全部项过期：下次 ensure 重读 */
  invalidate: () => void;
};

/** 等着合并发出的 ID，以及每个 ID 当前读取的完成信号 */
const queue = new Set<string>();
const pending = new Map<string, Promise<void>>();
/** 已排上、还没开始发请求的那次合并读取 */
let scheduled: Promise<void> | null = null;
/** 按后端上限切批 */
const chunk = <T>(items: T[], size: number): T[][] => {
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) out.push(items.slice(i, i + size));
  return out;
};

export const useCharactersStore = create<CharactersState>((set, get) => {
  /** 读一批并写进缓存：没返回的记为 missing，失败的保留旧内容并标 error */
  const load = async (ids: string[]) => {
    const now = Date.now();
    try {
      const cards = await getCharactersByIds(ids);
      const found = new Map(cards.map((card) => [card.id, card]));
      const next = { ...get().byId };
      for (const id of ids) {
        const card = found.get(id);
        next[id] = card
          ? { status: "ready", card, loadedAt: now }
          : { status: "missing", loadedAt: now };
      }
      set({ byId: next });
    } catch {
      const next = { ...get().byId };
      for (const id of ids) {
        next[id] = { status: "error", card: next[id]?.card, loadedAt: 0 };
      }
      set({ byId: next });
    }
  };

  /** 把队列里的 ID 按上限分批读完 */
  const flush = async () => {
    scheduled = null;
    const ids = [...queue];
    queue.clear();
    await Promise.all(chunk(ids, CHARACTER_IDS_LIMIT).map(load));
    for (const id of ids) pending.delete(id);
  };

  return {
    byId: {},

    ensure: (ids) => {
      const { byId } = get();
      const now = Date.now();
      const fresh: string[] = [];
      const loading: Record<string, CharacterEntry> = {};
      for (const id of new Set(ids)) {
        const entry = byId[id];
        if (pending.has(id) || queue.has(id)) continue;
        if (entry && entry.loadedAt !== 0 && now - entry.loadedAt < CHARACTER_TTL) continue;
        fresh.push(id);
        if (!entry) loading[id] = { status: "loading", loadedAt: 0 };
      }
      if (fresh.length > 0) {
        if (Object.keys(loading).length > 0) set({ byId: { ...byId, ...loading } });
        for (const id of fresh) queue.add(id);
        // 同一时刻挂载的一批节点各自 ensure，合并到下一个微任务统一发请求
        scheduled ??= Promise.resolve().then(flush);
        for (const id of fresh) pending.set(id, scheduled);
      }
      return Promise.all(ids.map((id) => pending.get(id))).then(() => undefined);
    },

    refresh: async (ids) => {
      const unique = [...new Set(ids)];
      await Promise.all(chunk(unique, CHARACTER_IDS_LIMIT).map(load));
      const { byId } = get();
      return Object.fromEntries(unique.map((id) => [id, byId[id]]));
    },

    upsert: (card) =>
      set({ byId: { ...get().byId, [card.id]: { status: "ready", card, loadedAt: Date.now() } } }),

    remove: (id) =>
      set({ byId: { ...get().byId, [id]: { status: "missing", loadedAt: Date.now() } } }),

    invalidate: () =>
      set({
        byId: Object.fromEntries(
          Object.entries(get().byId).map(([id, entry]) => [id, { ...entry, loadedAt: 0 }]),
        ),
      }),
  };
});
