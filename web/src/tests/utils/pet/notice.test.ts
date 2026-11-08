import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import { toast } from "sonner";

import { usePetStore } from "@/store/pet";
import { initialBubbleQueue } from "@/utils/pet/bubble-queue";
import { canvasNotice, petTakesNotices } from "@/utils/pet/notice";

const spies = {
  success: spyOn(toast, "success"),
  error: spyOn(toast, "error"),
  info: spyOn(toast, "info"),
  warning: spyOn(toast, "warning"),
};

beforeEach(() => {
  for (const s of Object.values(spies)) s.mockReset();
  usePetStore.setState({ active: false, queue: initialBubbleQueue(), reaction: null });
});
afterEach(() => {
  usePetStore.setState({ active: false });
});

describe("canvasNotice：桌宠不活跃时走 toast", () => {
  test("原样调用 sonner：kind、文案、id 与 action 都带上", () => {
    const action = { label: "打开", onClick: () => {} };
    canvasNotice("success", "已另存", { id: "save-as", action });
    expect(spies.success).toHaveBeenCalledTimes(1);
    expect(spies.success).toHaveBeenCalledWith("已另存", { id: "save-as", action });
    expect(usePetStore.getState().queue.current).toBeNull();
  });

  test("四种 kind 各调用自己的 toast 方法", () => {
    for (const kind of ["success", "error", "info", "warning"] as const) {
      canvasNotice(kind, "x");
      expect(spies[kind]).toHaveBeenCalledTimes(1);
    }
  });
});

describe("canvasNotice：浮窗打开时走 toast", () => {
  test("桌宠活跃但浮窗打开：画布上的小镜收起了，提醒退回 toast", () => {
    usePetStore.setState({ active: true });
    usePetStore.getState().setContext({ panelOpen: true });
    canvasNotice("error", "另存失败", { id: "save-as-failed" });
    expect(spies.error).toHaveBeenCalledWith("另存失败", {
      id: "save-as-failed",
      action: undefined,
    });
    expect(usePetStore.getState().queue.current).toBeNull();
  });

  test("petTakesNotices：只有活跃且浮窗收起时才由桌宠接管", () => {
    expect(petTakesNotices()).toBe(false);
    usePetStore.setState({ active: true });
    expect(petTakesNotices()).toBe(true);
    usePetStore.getState().setContext({ panelOpen: true });
    expect(petTakesNotices()).toBe(false);
  });
});

describe("canvasNotice：退回 toast 时的计数占位", () => {
  test("文案里的 {n} 换成 1，不把占位符原样显示", () => {
    canvasNotice("success", "{n} 个视频好了", { key: "result:success:c1" });
    expect(spies.success).toHaveBeenCalledWith("1 个视频好了", {
      id: undefined,
      action: undefined,
    });
  });

  test("桌宠活跃时 {n} 原样进气泡，由 bubbleText 按合并计数替换", () => {
    usePetStore.setState({ active: true });
    canvasNotice("success", "{n} 个视频好了");
    expect(usePetStore.getState().queue.current?.text).toBe("{n} 个视频好了");
  });
});

describe("canvasNotice：桌宠活跃时推气泡", () => {
  beforeEach(() => usePetStore.setState({ active: true }));

  test("不调用 toast，气泡带文案、id 和按钮", () => {
    const action = { label: "打开", onClick: () => {} };
    canvasNotice("success", "已另存", { id: "save-as", action });
    for (const s of Object.values(spies)) expect(s).not.toHaveBeenCalled();
    const cur = usePetStore.getState().queue.current!;
    expect(cur.id).toBe("save-as");
    expect(cur.text).toBe("已另存");
    expect(cur.action).toBe(action);
  });

  test("kind 对应关系：error 是失败动作 6 秒；success、info 是 idle 4 秒；warning 是 idle 6 秒", () => {
    const get = (kind: "success" | "error" | "info" | "warning") => {
      usePetStore.setState({ queue: initialBubbleQueue() });
      canvasNotice(kind, "x");
      return usePetStore.getState().queue.current!;
    };
    const e = get("error");
    expect([e.mood, e.ttl, e.priority, e.source]).toEqual(["failed", 6000, "error", "system"]);
    const s = get("success");
    expect([s.mood, s.ttl, s.priority, s.source]).toEqual(["idle", 4000, "system", "system"]);
    const i = get("info");
    expect([i.mood, i.ttl]).toEqual(["idle", 4000]);
    const w = get("warning");
    expect([w.mood, w.ttl, w.priority]).toEqual(["idle", 6000, "system"]);
  });

  test("opts 可以覆盖来源、优先级、动作和停留", () => {
    canvasNotice("success", "{n} 个视频好了", {
      id: "r1",
      key: "result:success:10",
      source: "result",
      priority: "result",
      mood: "jumping",
      ttl: 5000,
    });
    const cur = usePetStore.getState().queue.current!;
    expect([cur.source, cur.priority, cur.mood, cur.ttl, cur.key]).toEqual([
      "result",
      "result",
      "jumping",
      5000,
      "result:success:10",
    ]);
  });

  test("没给 id 时自动生成，互不相同", () => {
    canvasNotice("info", "a");
    const first = usePetStore.getState().queue.current!.id;
    canvasNotice("info", "b");
    const second = usePetStore.getState().queue.queue[0]!.id;
    expect(first).not.toBe(second);
  });

  test("同 id 再来：更新同一个气泡，不重复", () => {
    canvasNotice("info", "旧", { id: "same" });
    canvasNotice("info", "新", { id: "same" });
    expect(usePetStore.getState().queue.current?.text).toBe("新");
    expect(usePetStore.getState().queue.queue).toEqual([]);
  });
});
