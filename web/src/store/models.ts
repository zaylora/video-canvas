import { create } from "zustand";

import { getModels } from "@/api/model";
import type { ModelInfo } from "@/api/model/type";

export type ModelsEntry = {
  status: "idle" | "loading" | "ready" | "error";
  models: ModelInfo[];
  loadedAt: number;
};

const EMPTY: ModelsEntry = { status: "idle", models: [], loadedAt: 0 };

/** 清单缓存多久后允许后台刷新 */
export const MODELS_TTL = 5 * 60 * 1000;

type ModelsState = {
  byKind: Record<string, ModelsEntry>;
  /** 加载某种类的模型清单；缓存新鲜时不发请求，force 强制刷新 */
  load: (kind: string, force?: boolean) => Promise<void>;
};

const inflight = new Map<string, Promise<void>>();

export const useModelsStore = create<ModelsState>((set, get) => ({
  byKind: {},

  load: (kind, force = false) => {
    const current = get().byKind[kind] ?? EMPTY;
    const fresh =
      current.status === "ready" && Date.now() - current.loadedAt < MODELS_TTL;
    if (!force && fresh) return Promise.resolve();
    const pending = inflight.get(kind);
    if (pending) return pending;

    // 已有清单时后台刷新不切回 loading，免得节点闪一下「加载中」
    if (current.status !== "ready") {
      set({
        byKind: { ...get().byKind, [kind]: { ...current, status: "loading" } },
      });
    }
    const task = getModels(kind)
      .then((models) =>
        set({
          byKind: {
            ...get().byKind,
            [kind]: { status: "ready", models, loadedAt: Date.now() },
          },
        }),
      )
      .catch(() => {
        const latest = get().byKind[kind] ?? EMPTY;
        // 之前成功过的清单继续用，别因为一次刷新失败把节点全判成「已下线」
        set({
          byKind: {
            ...get().byKind,
            [kind]: {
              ...latest,
              status: latest.models.length > 0 ? "ready" : "error",
            },
          },
        });
      })
      .finally(() => {
        inflight.delete(kind);
      });
    inflight.set(kind, task);
    return task;
  },
}));
