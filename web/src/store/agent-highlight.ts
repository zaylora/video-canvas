import { create } from "zustand";

/** Agent 刚改过的节点描边停留多久（毫秒） */
export const TOUCH_MS = 1500;

/** 节点上的 Agent 标记：刚被 Agent 改过（紫色描边），或在待批准的删除卡片上被悬停（红色描边） */
export type AgentMark = "touched" | "danger";

type HighlightStore = {
  /** 节点 id → 标记；没有标记的节点不在里面 */
  marks: Record<string, AgentMark>;
  /** 标记一批节点为「刚被 Agent 改过」，TOUCH_MS 后自动取消；同一个节点连续被改时从最后一次重新计时 */
  touch: (ids: readonly string[]) => void;
  /** 标记一批节点为「将被删除」，传空数组取消；悬停期间一直显示 */
  setDanger: (ids: readonly string[]) => void;
};

/** 定时器（测试里换成假的） */
export const highlightClock = {
  set: (fn: () => void, ms: number): unknown => setTimeout(fn, ms),
  clear: (handle: unknown) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

const timers = new Map<string, unknown>();

/** 节点上的 Agent 描边状态：写入由 Agent 改动的合并和审批卡片的悬停触发，节点按自己的 id 订阅 */
export const useAgentHighlight = create<HighlightStore>((set, get) => ({
  marks: {},

  touch: (ids) => {
    if (ids.length === 0) return;
    set((s) => {
      const marks = { ...s.marks };
      // 悬停中的删除标记比「刚改过」更要紧，不被盖掉
      for (const id of ids) if (marks[id] !== "danger") marks[id] = "touched";
      return { marks };
    });
    for (const id of ids) {
      highlightClock.clear(timers.get(id));
      timers.set(
        id,
        highlightClock.set(() => {
          timers.delete(id);
          const marks = get().marks;
          if (marks[id] !== "touched") return;
          const { [id]: _gone, ...rest } = marks;
          set({ marks: rest });
        }, TOUCH_MS),
      );
    }
  },

  setDanger: (ids) =>
    set((s) => {
      const marks: Record<string, AgentMark> = {};
      for (const [id, mark] of Object.entries(s.marks)) if (mark !== "danger") marks[id] = mark;
      for (const id of ids) marks[id] = "danger";
      return { marks };
    }),
}));
