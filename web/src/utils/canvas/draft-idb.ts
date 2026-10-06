import { openDB, type IDBPDatabase } from "idb";

import { createDraftStore, type DraftBackend } from "./draft-store";

const DB_NAME = "video-canvas";
const STORE = "canvas-drafts";

let dbPromise: Promise<IDBPDatabase> | undefined;

/** 打开库；打开失败（隐私模式、被禁用）时清掉缓存，下次再试，错误交给上层按降级处理 */
function db() {
  dbPromise ??= openDB(DB_NAME, 1, {
    upgrade(database) {
      database.createObjectStore(STORE);
    },
  });
  dbPromise.catch(() => {
    dbPromise = undefined;
  });
  return dbPromise;
}

const idbBackend: DraftBackend = {
  get: async (key) => (await db()).get(STORE, key),
  put: async (key, value) => {
    await (await db()).put(STORE, value, key);
  },
  delete: async (key) => {
    await (await db()).delete(STORE, key);
  },
  keys: async () => (await (await db()).getAllKeys(STORE)).map(String),
};

/** 生产环境用的草稿存储：IndexedDB 后端 */
export const draftStore = createDraftStore(idbBackend);
