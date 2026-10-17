import { describe, expect, test } from "bun:test";

import { toolLabel, toolVerb } from "@/constants/agent";

describe("工具名称", () => {
  test("后端下发的名称优先；没有时用本地常量；都没有就显示原名", () => {
    expect(toolLabel("canvas_delete")).toBe("申请删除");
    expect(toolLabel("canvas_delete", "删除节点")).toBe("删除节点");
    expect(toolLabel("web_search", "搜索网页")).toBe("搜索网页");
    expect(toolLabel("web_search")).toBe("web_search");
  });

  test("动词：下发的动词 > 本地动词 > 下发的名称 > 本地名称 > 原名", () => {
    expect(toolVerb("canvas_delete")).toBe("删除");
    expect(toolVerb("canvas_delete", "移除")).toBe("移除");
    expect(toolVerb("web_search", undefined, "搜索网页")).toBe("搜索网页");
    expect(toolVerb("web_search")).toBe("web_search");
  });
});
