import { beforeEach, describe, expect, test } from "bun:test";

import { TOUCH_MS, highlightClock, useAgentHighlight } from "@/store/agent-highlight";

describe("Agent 描边标记", () => {
  /** 假时钟：按时间顺序触发到期的定时器 */
  let now = 0;
  let seq = 0;
  let pending = new Map<number, { at: number; fn: () => void }>();
  const advance = (ms: number) => {
    const target = now + ms;
    for (;;) {
      const next = [...pending.entries()]
        .filter(([, t]) => t.at <= target)
        .sort((a, b) => a[1].at - b[1].at)[0];
      if (!next) break;
      pending.delete(next[0]);
      now = next[1].at;
      next[1].fn();
    }
    now = target;
  };
  beforeEach(() => {
    now = 0;
    pending = new Map();
    highlightClock.set = (fn, ms) => {
      pending.set(++seq, { at: now + ms, fn });
      return seq;
    };
    highlightClock.clear = (h) => void pending.delete(h as number);
    useAgentHighlight.setState({ marks: {} });
  });

  test("刚被改过：TOUCH_MS 后自动取消", () => {
    useAgentHighlight.getState().touch(["a", "b"]);
    expect(useAgentHighlight.getState().marks).toEqual({ a: "touched", b: "touched" });
    advance(TOUCH_MS + 1);
    expect(useAgentHighlight.getState().marks).toEqual({});
  });

  test("连续被改：从最后一次重新计时", () => {
    useAgentHighlight.getState().touch(["a"]);
    advance(TOUCH_MS - 100);
    useAgentHighlight.getState().touch(["a"]);
    advance(200);
    expect(useAgentHighlight.getState().marks.a).toBe("touched");
    advance(TOUCH_MS);
    expect(useAgentHighlight.getState().marks.a).toBeUndefined();
  });

  test("悬停删除卡片：标红，换一批或清空时恢复；不影响别的节点的「刚改过」", () => {
    useAgentHighlight.getState().touch(["a"]);
    useAgentHighlight.getState().setDanger(["b", "c"]);
    expect(useAgentHighlight.getState().marks).toEqual({ a: "touched", b: "danger", c: "danger" });
    useAgentHighlight.getState().setDanger(["c"]);
    expect(useAgentHighlight.getState().marks).toEqual({ a: "touched", c: "danger" });
    useAgentHighlight.getState().setDanger([]);
    expect(useAgentHighlight.getState().marks).toEqual({ a: "touched" });
  });

  test("红色标记比「刚改过」更要紧：不被盖掉，也不被计时器清掉", () => {
    useAgentHighlight.getState().setDanger(["a"]);
    useAgentHighlight.getState().touch(["a"]);
    expect(useAgentHighlight.getState().marks.a).toBe("danger");
    advance(TOUCH_MS + 1);
    expect(useAgentHighlight.getState().marks.a).toBe("danger");
  });
});
