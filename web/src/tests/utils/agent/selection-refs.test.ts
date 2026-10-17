import { describe, expect, test } from "bun:test";

import type { NodeOption } from "@/pages/canvas/agent/agent-editor";
import { resolveSelection, splitSelection } from "@/utils/agent/selection-refs";

const nodes: NodeOption[] = [
  { id: "a", label: "图片 7", kind: "image" },
  { id: "b", label: "视频 2", kind: "video" },
  { id: "c", label: "", kind: "script" },
];

describe("引用的节点", () => {
  test("按选中的顺序解析成标题和种类；去重", () => {
    expect(resolveSelection(["b", "a", "b"], nodes)).toEqual([
      { id: "b", label: "视频 2", kind: "video", missing: false },
      { id: "a", label: "图片 7", kind: "image", missing: false },
    ]);
  });

  test("没有标题的节点用「种类 + 节点」兜底", () => {
    expect(resolveSelection(["c"], nodes)[0]).toMatchObject({ label: "文本节点", missing: false });
  });

  test("画布上已经没有的节点：标成已删除，不能点", () => {
    expect(resolveSelection(["gone"], nodes)).toEqual([
      { id: "gone", label: "已删除的节点", kind: undefined, missing: true },
    ]);
  });

  test("没有选中就是空", () => {
    expect(resolveSelection([], nodes)).toEqual([]);
  });
});

describe("折叠", () => {
  const ten = Array.from({ length: 10 }, (_, i) => i);

  test("不超过 3 个：全部显示，没有「+N」", () => {
    expect(splitSelection([1, 2, 3])).toEqual({ shown: [1, 2, 3], hidden: 0 });
    expect(splitSelection([])).toEqual({ shown: [], hidden: 0 });
  });

  test("超过 3 个：显示前 3 个，其余折成「+N」", () => {
    expect(splitSelection([1, 2, 3, 4])).toEqual({ shown: [1, 2, 3], hidden: 1 });
    expect(splitSelection(ten)).toEqual({ shown: [0, 1, 2], hidden: 7 });
  });

  test("展开后全部显示", () => {
    expect(splitSelection(ten, true)).toEqual({ shown: ten, hidden: 0 });
  });
});
