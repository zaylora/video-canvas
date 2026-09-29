import { create } from "zustand";

import { getCredits } from "@/api/credit";
import type { CreditsDto } from "@/api/credit/type";

type CreditsState = {
  credits: CreditsDto | null;
  /** 拉最新余额；失败时保留旧值 */
  refresh: () => Promise<void>;
};

let inflight: Promise<void> | null = null;

export const useCreditsStore = create<CreditsState>((set) => ({
  credits: null,
  refresh: () => {
    // 同一时刻多处触发只发一次请求
    inflight ??= getCredits()
      .then((credits) => set({ credits }))
      .catch(() => undefined)
      .finally(() => {
        inflight = null;
      });
    return inflight;
  },
}));
