import { create } from "zustand";

import {
  bubbleQueue,
  initialBubbleQueue,
  type BubbleContext,
  type BubbleInput,
  type BubbleQueueAction,
  type BubbleQueueState,
} from "@/utils/pet/bubble-queue";
import type { PetAssetsStatus } from "@/utils/pet/assets-status";
import { nextReaction, type PetReaction, type PetReactionKind } from "@/utils/pet/mood";
import type { Rect, Size } from "@/utils/pet/placement";

type PetState = {
  /** 桌宠是否在接管画布提醒：档位不是隐藏、图集已加载、桌宠已挂载，由界面层写入 */
  active: boolean;
  /** 素材加载状态，由界面层写入：外壳据此决定右下角画不画胶囊、给桌宠留不留位 */
  status: PetAssetsStatus;
  /** 气泡队列状态（bubble-queue 的 reducer）；current 是正在显示的那条 */
  queue: BubbleQueueState;
  /** 一次性反应，播完由界面层 clearReaction */
  reaction: PetReaction | null;
  /** 桌宠在画布区域里的位置（区域坐标）和区域尺寸，浮窗据此跟着桌宠弹出；不活跃时是 null */
  placement: { rect: Rect; area: Size } | null;

  setActive: (active: boolean) => void;
  setStatus: (status: PetAssetsStatus) => void;
  setPlacement: (placement: { rect: Rect; area: Size } | null) => void;
  /** 推一条气泡；now 缺省取当前时间 */
  push: (bubble: BubbleInput, now?: number) => void;
  /** 关闭气泡：不给 id 是关闭当前这条 */
  dismiss: (id?: string, now?: number) => void;
  /** 写入界面层的上下文（档位、拖动、浮窗开关、助手状态、页面可见性） */
  setContext: (patch: Partial<BubbleContext>, now?: number) => void;
  /** 触发一次性反应；播放期间同类再来不重新开始 */
  react: (kind: PetReactionKind, now?: number) => void;
  clearReaction: () => void;
};

/** 气泡出现时的动作对应的反应；waving、sleeping 等由渲染层自己处理 */
const BUBBLE_REACTION: Partial<Record<string, PetReactionKind>> = {
  jumping: "celebrate",
  failed: "failed",
};

/**
 * 桌宠的运行时状态：不持久化。队列规则都在 bubble-queue 里，这里只负责存和在气泡
 * 新成为当前时按它的动作触发反应。
 */
export const usePetStore = create<PetState>((set, get) => {
  const dispatch = (action: BubbleQueueAction, now: number) => {
    const prev = get().queue;
    const next = bubbleQueue(prev, action);
    if (next === prev) return;
    const changed = next.current && !sameBubble(prev.current, next.current);
    const kind = changed ? BUBBLE_REACTION[next.current!.mood] : undefined;
    set((s) => ({
      queue: next,
      reaction: kind ? nextReaction(s.reaction, kind, now) : s.reaction,
    }));
  };

  return {
    active: false,
    status: "off",
    queue: initialBubbleQueue(),
    reaction: null,
    placement: null,

    setActive: (active) => {
      set(active ? { active } : { active, placement: null });
      if (!active) dispatch({ type: "clear" }, Date.now());
    },
    setStatus: (status) => set({ status }),
    setPlacement: (placement) => set({ placement }),
    push: (bubble, now = Date.now()) => dispatch({ type: "push", bubble, now }, now),
    dismiss: (id, now = Date.now()) => dispatch({ type: "dismiss", id, now }, now),
    setContext: (patch, now = Date.now()) => dispatch({ type: "context", patch, now }, now),
    react: (kind, now = Date.now()) =>
      set((s) => ({ reaction: nextReaction(s.reaction, kind, now) })),
    clearReaction: () => set({ reaction: null }),
  };
});

/** 是不是同一条气泡的同一版（id 和版本号都相同） */
const sameBubble = (
  a: { id: string; version: number } | null,
  b: { id: string; version: number },
) => a !== null && a.id === b.id && a.version === b.version;
