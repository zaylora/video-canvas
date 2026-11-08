import { describe, expect, test } from "bun:test";

import {
  EASTER_COOLDOWN_MS,
  MAX_QUEUE,
  MERGE_WINDOW_MS,
  bubbleQueue,
  bubbleText,
  initialBubbleQueue,
  type BubbleContext,
  type BubbleInput,
  type BubbleQueueAction,
  type BubbleQueueState,
} from "@/utils/pet/bubble-queue";

let seq = 0;
const make = (patch: Partial<BubbleInput> = {}): BubbleInput => ({
  id: `b${++seq}`,
  source: "system",
  priority: "system",
  text: "文案",
  mood: "idle",
  ttl: 4000,
  ...patch,
});

const run = (state: BubbleQueueState, ...actions: BubbleQueueAction[]) =>
  actions.reduce(bubbleQueue, state);
const push = (bubble: BubbleInput, now = 0): BubbleQueueAction => ({ type: "push", bubble, now });
const ctx = (patch: Partial<BubbleContext>, now = 0): BubbleQueueAction => ({
  type: "context",
  patch,
  now,
});
const ids = (s: BubbleQueueState) => s.queue.map((b) => b.id);

describe("基本显示", () => {
  test("空队列推一条：直接成为当前气泡", () => {
    const b = make();
    const s = run(initialBubbleQueue(), push(b, 10));
    expect(s.current?.id).toBe(b.id);
    expect(s.current?.count).toBe(1);
    expect(s.queue).toEqual([]);
  });

  test("一次只显示一个：同级后来的排队", () => {
    const a = make();
    const b = make();
    const s = run(initialBubbleQueue(), push(a), push(b));
    expect(s.current?.id).toBe(a.id);
    expect(ids(s)).toEqual([b.id]);
  });

  test("关闭当前后，下一条补上", () => {
    const a = make();
    const b = make();
    const s = run(initialBubbleQueue(), push(a), push(b), { type: "dismiss", now: 1 });
    expect(s.current?.id).toBe(b.id);
    expect(s.queue).toEqual([]);
  });

  test("按 id 关闭排队中的气泡", () => {
    const a = make();
    const b = make();
    const s = run(initialBubbleQueue(), push(a), push(b), { type: "dismiss", id: b.id, now: 1 });
    expect(s.current?.id).toBe(a.id);
    expect(s.queue).toEqual([]);
  });

  test("补位时取优先级最高的，同级先到先出", () => {
    const cur = make({ priority: "approval", source: "assistant" });
    const low = make({ priority: "system" });
    const mid1 = make({ priority: "result", source: "result" });
    const mid2 = make({ priority: "result", source: "result" });
    const s = run(initialBubbleQueue(), push(cur), push(low), push(mid1), push(mid2), {
      type: "dismiss",
      now: 1,
    });
    expect(s.current?.id).toBe(mid1.id);
    expect(ids(s)).toEqual([low.id, mid2.id]);
  });

  test("bubbleText 把 {n} 换成合并计数", () => {
    const s = run(
      initialBubbleQueue(),
      push(make({ id: "x", key: "k", text: "{n} 个视频好了" }), 0),
      push(make({ id: "y", key: "k", text: "{n} 个视频好了" }), 100),
    );
    expect(bubbleText(s.current!)).toBe("2 个视频好了");
  });
});

describe("优先级抢占", () => {
  test("高优先级替换正在显示的低优先级，被替换的放回队首", () => {
    const sys = make({ priority: "system" });
    const other = make({ priority: "system" });
    const res = make({ priority: "result", source: "result" });
    const s = run(initialBubbleQueue(), push(sys), push(other), push(res));
    expect(s.current?.id).toBe(res.id);
    expect(ids(s)).toEqual([sys.id, other.id]);
  });

  test("同级不抢占", () => {
    const a = make({ priority: "result", source: "result" });
    const b = make({ priority: "result", source: "result" });
    const s = run(initialBubbleQueue(), push(a), push(b));
    expect(s.current?.id).toBe(a.id);
  });

  test("低级不抢占", () => {
    const a = make({ priority: "error" });
    const b = make({ priority: "result", source: "result" });
    expect(run(initialBubbleQueue(), push(a), push(b)).current?.id).toBe(a.id);
  });

  test("优先级顺序：审批 > 错误 > 结果 > 系统 > 彩蛋", () => {
    const order: BubbleInput["priority"][] = ["easter", "system", "result", "error", "approval"];
    let s = initialBubbleQueue();
    for (const p of order) {
      s = run(s, push(make({ priority: p, source: p === "easter" ? "easter" : "system" })));
      expect(s.current?.priority).toBe(p);
    }
  });

  test("被抢占的彩蛋不放回队列", () => {
    const egg = make({ priority: "easter", source: "easter" });
    const sys = make({ priority: "system" });
    const s = run(initialBubbleQueue(), push(egg), push(sys));
    expect(s.current?.id).toBe(sys.id);
    expect(s.queue).toEqual([]);
  });
});

describe("合并与去重", () => {
  test("同一个 key 2 秒内合并计数，文案保持第一条", () => {
    const s = run(
      initialBubbleQueue(),
      push(make({ id: "a", key: "result:ok", text: "第一条" }), 1000),
      push(make({ id: "b", key: "result:ok", text: "第二条" }), 1000 + MERGE_WINDOW_MS),
    );
    expect(s.current?.count).toBe(2);
    expect(s.current?.text).toBe("第一条");
    expect(s.queue).toEqual([]);
  });

  test("合并后窗口顺延：最后一次合并起 2 秒内继续合并", () => {
    const k = "k";
    const s = run(
      initialBubbleQueue(),
      push(make({ key: k }), 0),
      push(make({ key: k }), 1500),
      push(make({ key: k }), 3000),
    );
    expect(s.current?.count).toBe(3);
  });

  test("超过 2 秒不合并，作为新气泡排队", () => {
    const s = run(
      initialBubbleQueue(),
      push(make({ key: "k" }), 0),
      push(make({ key: "k" }), MERGE_WINDOW_MS + 1),
    );
    expect(s.current?.count).toBe(1);
    expect(s.queue.length).toBe(1);
  });

  test("合并也会合进排队中的同 key 气泡", () => {
    const s = run(
      initialBubbleQueue(),
      push(make({ priority: "error" }), 0),
      push(make({ key: "k" }), 0),
      push(make({ key: "k" }), 500),
    );
    expect(s.queue.length).toBe(1);
    expect(s.queue[0]!.count).toBe(2);
  });

  test("合并会让显示中的气泡版本号加一（界面据此重新计时）", () => {
    const s1 = run(initialBubbleQueue(), push(make({ key: "k" }), 0));
    const s2 = run(s1, push(make({ key: "k" }), 100));
    expect(s2.current!.version).toBe(s1.current!.version + 1);
  });

  test("同一个 id 去重：不重复入队，内容更新", () => {
    const s = run(
      initialBubbleQueue(),
      push(make({ id: "same", text: "旧" }), 0),
      push(make({ id: "same", text: "新" }), 10_000),
    );
    expect(s.current?.text).toBe("新");
    expect(s.current?.count).toBe(1);
    expect(s.queue).toEqual([]);
  });

  test("去重也作用于排队中的气泡", () => {
    const s = run(
      initialBubbleQueue(),
      push(make({ priority: "error" })),
      push(make({ id: "same", text: "旧" })),
      push(make({ id: "same", text: "新" }), 10_000),
    );
    expect(s.queue.length).toBe(1);
    expect(s.queue[0]!.text).toBe("新");
  });
});

describe("队列上限", () => {
  test("最多排 5 条；超出时丢掉优先级最低、时间最早的", () => {
    const cur = make({ priority: "approval", source: "assistant" });
    const oldLow = make({ priority: "system" });
    const mids = Array.from({ length: 4 }, () => make({ priority: "result", source: "result" }));
    let s = run(initialBubbleQueue(), push(cur, 0), push(oldLow, 1));
    mids.forEach((m, i) => (s = run(s, push(m, 2 + i))));
    expect(s.queue.length).toBe(MAX_QUEUE);
    // 再来一条结果：system 级的 oldLow 被挤掉
    const extra = make({ priority: "result", source: "result" });
    s = run(s, push(extra, 10));
    expect(s.queue.length).toBe(MAX_QUEUE);
    expect(ids(s)).not.toContain(oldLow.id);
    expect(ids(s)).toContain(extra.id);
  });

  test("同级时丢最早的", () => {
    const cur = make({ priority: "approval", source: "assistant" });
    const items = Array.from({ length: MAX_QUEUE + 1 }, () => make({ priority: "system" }));
    let s = run(initialBubbleQueue(), push(cur, 0));
    items.forEach((m, i) => (s = run(s, push(m, 10 + i))));
    expect(s.queue.length).toBe(MAX_QUEUE);
    expect(ids(s)).toEqual(items.slice(1).map((b) => b.id));
  });
});

describe("彩蛋", () => {
  const egg = (patch: Partial<BubbleInput> = {}) =>
    make({ priority: "easter", source: "easter", ...patch });

  test("活泼档可以冒；30 分钟内最多一条，冷却过了再冒", () => {
    let s = run(initialBubbleQueue(), push(egg(), 0));
    expect(s.current?.source).toBe("easter");
    s = run(s, { type: "dismiss", now: 1 });
    s = run(s, push(egg(), EASTER_COOLDOWN_MS - 1));
    expect(s.current).toBeNull();
    s = run(s, push(egg(), EASTER_COOLDOWN_MS));
    expect(s.current?.source).toBe("easter");
  });

  test("安静档、隐藏档不冒彩蛋", () => {
    expect(run(initialBubbleQueue(), ctx({ mode: "quiet" }), push(egg())).current).toBeNull();
    expect(run(initialBubbleQueue(), ctx({ mode: "hidden" }), push(egg())).current).toBeNull();
  });

  test("拖动中、浮窗打开、助手忙碌、等审批时不冒", () => {
    for (const patch of [
      { dragging: true },
      { panelOpen: true },
      { assistantBusy: true },
      { assistantPending: true },
    ] as Partial<BubbleContext>[]) {
      const s = run(initialBubbleQueue(), ctx(patch), push(egg()));
      expect(s.current).toBeNull();
      expect(s.queue).toEqual([]);
    }
  });

  test("被拒的彩蛋不占冷却", () => {
    let s = run(initialBubbleQueue(), ctx({ assistantBusy: true }), push(egg(), 0));
    s = run(s, ctx({ assistantBusy: false }), push(egg(), 1));
    expect(s.current?.source).toBe("easter");
  });

  test("彩蛋显示中，助手开始忙碌：彩蛋收起", () => {
    let s = run(initialBubbleQueue(), push(egg()));
    s = run(s, ctx({ assistantBusy: true }));
    expect(s.current).toBeNull();
  });
});

describe("拖动", () => {
  test("拖动开始：当前气泡收起放回队首（彩蛋直接丢）", () => {
    const a = make({ priority: "result", source: "result" });
    let s = run(initialBubbleQueue(), push(a), ctx({ dragging: true }));
    expect(s.current).toBeNull();
    expect(ids(s)).toEqual([a.id]);
    const egg = make({ priority: "easter", source: "easter" });
    s = run(initialBubbleQueue(), push(egg), ctx({ dragging: true }));
    expect(s.current).toBeNull();
    expect(s.queue).toEqual([]);
  });

  test("拖动中来的消息先排队，松手后显示", () => {
    const a = make();
    let s = run(initialBubbleQueue(), ctx({ dragging: true }), push(a));
    expect(s.current).toBeNull();
    expect(ids(s)).toEqual([a.id]);
    s = run(s, ctx({ dragging: false }));
    expect(s.current?.id).toBe(a.id);
  });
});

describe("浮窗打开", () => {
  // 浮窗打开时画布上的小镜收起，气泡没处显示：这期间什么都不收，画布提醒由 canvasNotice 改走 toast
  test("打开时不收任何气泡", () => {
    const assistant = make({ source: "assistant", priority: "approval" });
    const result = make({ source: "result", priority: "result" });
    const s = run(initialBubbleQueue(), ctx({ panelOpen: true }), push(assistant), push(result));
    expect(s.current).toBeNull();
    expect(s.queue).toEqual([]);
  });

  test("打开浮窗：已显示和排队中的气泡全部收起，关上后不会再冒出来", () => {
    const assistant = make({ source: "assistant", priority: "approval" });
    const sys = make();
    let s = run(initialBubbleQueue(), push(assistant), push(sys));
    s = run(s, ctx({ panelOpen: true }));
    expect(s.current).toBeNull();
    expect(s.queue).toEqual([]);
    s = run(s, ctx({ panelOpen: false }));
    expect(s.current).toBeNull();
  });
});

describe("页面回到前台", () => {
  test("只保留优先级最高的一条，其余丢弃", () => {
    const a = make({ priority: "system" });
    const b = make({ priority: "result", source: "result" });
    const c = make({ priority: "error" });
    const d = make({ priority: "system" });
    let s = run(
      initialBubbleQueue(),
      ctx({ pageVisible: false }),
      push(a),
      push(b),
      push(c),
      push(d),
    );
    s = run(s, ctx({ pageVisible: true }));
    expect(s.current?.id).toBe(c.id);
    expect(s.queue).toEqual([]);
  });

  test("同级时保留正在显示的那条", () => {
    const a = make();
    const b = make();
    let s = run(initialBubbleQueue(), ctx({ pageVisible: false }), push(a), push(b));
    s = run(s, ctx({ pageVisible: true }));
    expect(s.current?.id).toBe(a.id);
    expect(s.queue).toEqual([]);
  });

  test("页面本来就在前台时，更新其他上下文不会丢气泡", () => {
    const a = make();
    const b = make();
    let s = run(initialBubbleQueue(), push(a), push(b));
    s = run(s, ctx({ pageVisible: true, assistantBusy: true }));
    expect(ids(s)).toEqual([b.id]);
  });
});

describe("清空", () => {
  test("clear 清掉当前和排队，保留上下文与彩蛋冷却", () => {
    const egg = make({ priority: "easter", source: "easter" });
    let s = run(
      initialBubbleQueue(),
      ctx({ mode: "quiet" }),
      ctx({ mode: "lively" }),
      push(egg, 5),
    );
    s = run(s, push(make()), { type: "clear" });
    expect(s.current).toBeNull();
    expect(s.queue).toEqual([]);
    expect(s.lastEasterAt).toBe(5);
    expect(s.ctx.mode).toBe("lively");
  });
});
