import { describe, expect, test } from "bun:test";

import { isEdgeHighlighted, shouldShowSweep } from "@/utils/canvas/edge-sweep";

describe("shouldShowSweep：连线流光只在选中相关元素时出现", () => {
  const idle = {
    edgeSelected: false,
    sourceSelected: false,
    targetSelected: false,
    running: false,
  };

  test("什么都没选中、下游也没在生成时，不显示流光", () => {
    expect(shouldShowSweep(idle)).toBe(false);
  });

  test("选中这条连线时显示", () => {
    expect(shouldShowSweep({ ...idle, edgeSelected: true })).toBe(true);
  });

  test("选中上游节点时，它连出去的线显示", () => {
    expect(shouldShowSweep({ ...idle, sourceSelected: true })).toBe(true);
  });

  test("选中下游节点时，连进来的线显示", () => {
    expect(shouldShowSweep({ ...idle, targetSelected: true })).toBe(true);
  });

  test("下游正在生成时，即使没选中也显示，提示这根线在喂数据", () => {
    expect(shouldShowSweep({ ...idle, running: true })).toBe(true);
  });
});

describe("isEdgeHighlighted：选中相关元素时连线换成品牌色", () => {
  const none = { edgeSelected: false, sourceSelected: false, targetSelected: false };

  test("什么都没选中时保持默认颜色", () => {
    expect(isEdgeHighlighted(none)).toBe(false);
  });

  test("选中连线本身、上游或下游节点时高亮", () => {
    expect(isEdgeHighlighted({ ...none, edgeSelected: true })).toBe(true);
    expect(isEdgeHighlighted({ ...none, sourceSelected: true })).toBe(true);
    expect(isEdgeHighlighted({ ...none, targetSelected: true })).toBe(true);
  });
});
