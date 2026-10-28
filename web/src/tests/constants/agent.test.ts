import { describe, expect, test } from "bun:test";

import { runStatusText, toolLabel, toolVerb } from "@/constants/agent";

describe("工具名称", () => {
  test("后端下发的名称优先；没有时用本地常量；都没有就显示原名", () => {
    expect(toolLabel("canvas_delete")).toBe("申请删除");
    expect(toolLabel("canvas_delete", "删除节点")).toBe("删除节点");
    expect(toolLabel("new_tool", "新工具")).toBe("新工具");
    expect(toolLabel("new_tool")).toBe("new_tool");
  });

  test("动词：下发的动词 > 本地动词 > 下发的名称 > 本地名称 > 原名", () => {
    expect(toolVerb("canvas_delete")).toBe("删除");
    expect(toolVerb("canvas_delete", "移除")).toBe("移除");
    expect(toolVerb("new_tool", undefined, "新工具")).toBe("新工具");
    expect(toolVerb("new_tool")).toBe("new_tool");
  });
});

describe("运行状态的文字", () => {
  test("后端随事件下发的标题和提示优先；没下发时（旧事件）用本地的；都没有就是空", () => {
    expect(runStatusText("canceled", "已停止（新文案）", "新的提示")).toEqual({
      title: "已停止（新文案）",
      hint: "新的提示",
    });
    expect(runStatusText("canceled")).toEqual({
      title: "已停止",
      hint: "没做完的计划还留着，可以接着让它做",
    });
    expect(runStatusText("canceled", "", "")).toEqual(runStatusText("canceled"));
    expect(runStatusText("some_new_status", "新状态", "新提示")).toEqual({
      title: "新状态",
      hint: "新提示",
    });
    expect(runStatusText("some_new_status")).toBeUndefined();
  });

  test("只下发了标题没下发提示：提示退回本地的，再没有就是空串", () => {
    expect(runStatusText("failed", "出错了")).toEqual({
      title: "出错了",
      hint: "可以换个说法重试",
    });
    expect(runStatusText("some_new_status", "新状态")).toEqual({ title: "新状态", hint: "" });
  });
});
