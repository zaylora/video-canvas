import type { PetMode } from "./mood";

/** 同一个 key 的消息在这段时间内合并计数（毫秒） */
export const MERGE_WINDOW_MS = 2000;
/** 排队的气泡最多几条（不含正在显示的） */
export const MAX_QUEUE = 5;
/** 彩蛋冷却：这段时间内最多一条 */
export const EASTER_COOLDOWN_MS = 30 * 60 * 1000;

/** 优先级：助手审批 > 错误 > 结果 > 系统 > 彩蛋 */
export type BubblePriority = "approval" | "error" | "result" | "system" | "easter";
export const PRIORITY_RANK: Record<BubblePriority, number> = {
  approval: 4,
  error: 3,
  result: 2,
  system: 1,
  easter: 0,
};

/** 来源类别：助手（浮窗打开时不冒）、生成结果、系统提醒、彩蛋 */
export type BubbleSource = "assistant" | "result" | "system" | "easter";

/** 气泡出现时桌宠的动作，由渲染层 / store 据此触发反应 */
export type BubbleMood = "idle" | "waving" | "jumping" | "failed" | "waiting" | "sleeping";

/** 气泡上的按钮，形状与 sonner 的 action 一致 */
export type BubbleAction = { label: string; onClick: () => void };

/** 推一条气泡需要给的内容 */
export type BubbleInput = {
  /** 去重用，沿用原 toast 的 id */
  id: string;
  /** 合并用，例如 result:success:{canvasId}；不给则不合并 */
  key?: string;
  source: BubbleSource;
  priority: BubblePriority;
  /** 文案，可含 {n} 占位，显示时换成合并计数（见 bubbleText） */
  text: string;
  mood: BubbleMood;
  /** 停留毫秒数，null 是不自动消失 */
  ttl: number | null;
  /** 气泡上的按钮 */
  action?: BubbleAction;
  /** 点击气泡本身 */
  onClick?: () => void;
};

/** 队列里的气泡：内容加队列维护的字段 */
export type Bubble = BubbleInput & {
  /** 合并了几条 */
  count: number;
  createdAt: number;
  /** 最近一次合并 / 更新的时间，合并窗口从这里算起 */
  updatedAt: number;
  /** 每次合并或更新加一，界面据此重新计时 */
  version: number;
};

/** 界面层写入的上下文，决定哪些气泡能冒 */
export type BubbleContext = {
  mode: PetMode;
  dragging: boolean;
  /** 助手浮窗是否打开 */
  panelOpen: boolean;
  assistantBusy: boolean;
  assistantPending: boolean;
  /** 页面是否在前台 */
  pageVisible: boolean;
};

export type BubbleQueueState = {
  /** 正在显示的气泡 */
  current: Bubble | null;
  /** 排队中的气泡，靠前的先补位（同优先级内） */
  queue: Bubble[];
  /** 上一条彩蛋入队的时间，用来算冷却 */
  lastEasterAt: number | null;
  ctx: BubbleContext;
};

export type BubbleQueueAction =
  | { type: "push"; bubble: BubbleInput; now: number }
  /** 不给 id 是关闭当前气泡 */
  | { type: "dismiss"; id?: string; now: number }
  | { type: "context"; patch: Partial<BubbleContext>; now: number }
  /** 清掉所有气泡，保留上下文和彩蛋冷却 */
  | { type: "clear" };

export const initialBubbleQueue = (): BubbleQueueState => ({
  current: null,
  queue: [],
  lastEasterAt: null,
  ctx: {
    mode: "lively",
    dragging: false,
    panelOpen: false,
    assistantBusy: false,
    assistantPending: false,
    pageVisible: true,
  },
});

/** 文案里的 {n} 换成合并计数 */
export const bubbleText = (b: Bubble): string => b.text.replaceAll("{n}", String(b.count));

const rank = (b: Bubble) => PRIORITY_RANK[b.priority];
const isEaster = (b: Bubble | BubbleInput) => b.source === "easter";

/** 当前上下文下不能冒彩蛋：不是活泼档、拖动中、浮窗打开、助手忙或在等审批 */
const easterBlocked = (c: BubbleContext) =>
  c.mode !== "lively" || c.dragging || c.panelOpen || c.assistantBusy || c.assistantPending;

/** 排队里下一个该补位的下标：优先级最高，同级先到先出；空返回 -1 */
function nextIndex(queue: readonly Bubble[]): number {
  let best = -1;
  queue.forEach((b, i) => {
    if (best < 0 || rank(b) > rank(queue[best]!)) best = i;
  });
  return best;
}

/** 当前位置空着且没在拖动时，从排队里补一条上来；顺带丢掉已不允许冒的 */
function promote(state: BubbleQueueState): BubbleQueueState {
  if (state.current || state.ctx.dragging) return state;
  const { ctx } = state;
  const queue = state.queue.filter(
    (b) => !(isEaster(b) && easterBlocked(ctx)) && !(b.source === "assistant" && ctx.panelOpen),
  );
  const i = nextIndex(queue);
  if (i < 0) return { ...state, queue };
  return { ...state, current: queue[i]!, queue: queue.filter((_, j) => j !== i) };
}

/** 超出上限时丢优先级最低、时间最早的 */
function capQueue(queue: Bubble[]): Bubble[] {
  const next = [...queue];
  while (next.length > MAX_QUEUE) {
    let drop = 0;
    next.forEach((b, i) => {
      const d = next[drop]!;
      if (rank(b) < rank(d) || (rank(b) === rank(d) && b.createdAt < d.createdAt)) drop = i;
    });
    next.splice(drop, 1);
  }
  return next;
}

/** 当前气泡让位：放回队首，彩蛋直接丢 */
function putBack(state: BubbleQueueState): BubbleQueueState {
  const { current } = state;
  if (!current) return state;
  return {
    ...state,
    current: null,
    queue: isEaster(current) ? state.queue : [current, ...state.queue],
  };
}

/** 去掉满足条件的气泡（当前的和排队的） */
function removeWhere(state: BubbleQueueState, test: (b: Bubble) => boolean): BubbleQueueState {
  return {
    ...state,
    current: state.current && test(state.current) ? null : state.current,
    queue: state.queue.filter((b) => !test(b)),
  };
}

/** 页面回到前台：只留优先级最高的一条（同级留正在显示的，其次最早的） */
function keepTop(state: BubbleQueueState): BubbleQueueState {
  const all = state.current ? [state.current, ...state.queue] : state.queue;
  const best = all[nextIndex(all)];
  if (!best) return state;
  if (state.ctx.dragging) return { ...state, current: null, queue: [best] };
  return { ...state, current: best, queue: [] };
}

/** 在当前和排队的气泡里找满足条件的第一条，返回更新它的结果；没找到返回 null */
function updateMatching(
  state: BubbleQueueState,
  match: (b: Bubble) => boolean,
  update: (b: Bubble) => Bubble,
): BubbleQueueState | null {
  if (state.current && match(state.current)) return { ...state, current: update(state.current) };
  const i = state.queue.findIndex(match);
  if (i < 0) return null;
  return { ...state, queue: state.queue.map((b, j) => (j === i ? update(b) : b)) };
}

function push(state: BubbleQueueState, input: BubbleInput, now: number): BubbleQueueState {
  const { ctx } = state;
  // 浮窗打开时画布上的小镜收起，气泡没处显示：什么都不收
  if (ctx.panelOpen) return state;
  if (isEaster(input)) {
    if (easterBlocked(ctx)) return state;
    if (state.lastEasterAt !== null && now - state.lastEasterAt < EASTER_COOLDOWN_MS) return state;
  }

  // 同一个 id：更新内容，不重复入队
  const byId = updateMatching(
    state,
    (b) => b.id === input.id,
    (old) => ({
      ...input,
      count: old.count,
      createdAt: old.createdAt,
      updatedAt: now,
      version: old.version + 1,
    }),
  );
  if (byId) return byId;

  // 同一个 key 在合并窗口内：计数加一，文案保持第一条
  if (input.key !== undefined) {
    const merged = updateMatching(
      state,
      (b) => b.key === input.key && now - b.updatedAt <= MERGE_WINDOW_MS,
      (old) => ({ ...old, count: old.count + 1, updatedAt: now, version: old.version + 1 }),
    );
    if (merged) return merged;
  }

  const bubble: Bubble = { ...input, count: 1, createdAt: now, updatedAt: now, version: 1 };
  const lastEasterAt = isEaster(bubble) ? now : state.lastEasterAt;
  const base = { ...state, lastEasterAt };

  if (!base.current && !ctx.dragging) return { ...base, current: bubble };
  if (base.current && rank(bubble) > rank(base.current)) {
    const yielded = putBack(base);
    return { ...yielded, current: bubble, queue: capQueue(yielded.queue) };
  }
  return { ...base, queue: capQueue([...base.queue, bubble]) };
}

/**
 * 气泡队列 reducer（纯函数）。一次只显示一个：高优先级直接替换低优先级，
 * 被替换的放回队首（彩蛋除外）；同 key 2 秒内合并计数、同 id 去重、队列上限 5。
 * 时间一律由 action 带入，这里不读时钟。
 */
export function bubbleQueue(state: BubbleQueueState, action: BubbleQueueAction): BubbleQueueState {
  switch (action.type) {
    case "push":
      return promote(push(state, action.bubble, action.now));

    case "dismiss": {
      if (action.id === undefined || action.id === state.current?.id) {
        return promote({ ...state, current: null });
      }
      const id = action.id;
      return { ...state, queue: state.queue.filter((b) => b.id !== id) };
    }

    case "context": {
      const prev = state.ctx;
      let next: BubbleQueueState = { ...state, ctx: { ...prev, ...action.patch } };
      const { ctx } = next;
      // 浮窗打开：已显示和排队的全部收起，免得计时停住、关上后又冒出旧消息
      if (ctx.panelOpen) next = { ...next, current: null, queue: [] };
      if (easterBlocked(ctx)) next = removeWhere(next, isEaster);
      if (ctx.dragging && !prev.dragging) next = putBack(next);
      if (ctx.pageVisible && !prev.pageVisible) next = keepTop(next);
      return promote(next);
    }

    case "clear":
      return { ...state, current: null, queue: [] };
  }
}
