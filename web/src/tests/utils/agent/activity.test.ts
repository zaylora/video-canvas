import { describe, expect, test } from "bun:test";

import { activitySummary } from "@/utils/agent/activity";
import type { ToolCall } from "@/utils/agent/timeline";

const tool = (name: string, extra: Partial<ToolCall> = {}): ToolCall => ({
  key: `${name}-${Math.random()}`,
  name,
  status: "done",
  summary: "",
  nodeIds: [],
  ...extra,
});

describe("activitySummary：活动行收起后的图标摘要", () => {
  test("按动词首次出现的顺序计数，带图标键", () => {
    const got = activitySummary({
      tools: [
        tool("plan_update"),
        tool("canvas_get_state"),
        tool("plan_update"),
        tool("canvas_apply_ops"),
      ],
      status: "done",
      startedAt: 0,
      endedAt: 69_000,
    });
    expect(got.seconds).toBe(69);
    expect(got.verbs).toEqual([
      { verb: "计划", icon: "plan", count: 2 },
      { verb: "读取", icon: "read", count: 1 },
      { verb: "修改", icon: "edit", count: 1 },
    ]);
  });

  test("后端下发的动词优先；不认识的动词用通用图标", () => {
    const got = activitySummary({
      tools: [tool("codemode", { verb: "脚本" }), tool("new_tool", { verb: "冥想" })],
      status: "done",
      startedAt: null,
      endedAt: null,
    });
    expect(got.seconds).toBeNull();
    expect(got.verbs.map((v) => v.icon)).toEqual(["script", "other"]);
  });

  test("用时至少 1 秒", () => {
    const got = activitySummary({ tools: [], status: "done", startedAt: 0, endedAt: 200 });
    expect(got.seconds).toBe(1);
  });

  test("文字版摘要给读屏和 tooltip 用", () => {
    const got = activitySummary({
      tools: [tool("ask_user"), tool("canvas_apply_ops")],
      status: "aborted",
      startedAt: 0,
      endedAt: 27_000,
    });
    expect(got.text).toBe("已中止 · 提问 1 · 修改 1");
  });
});
