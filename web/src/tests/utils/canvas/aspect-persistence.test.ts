import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import { deserializeGraph, serializeGraph } from "@/utils/canvas/canvas-persistence";

const viewport = { x: 0, y: 0, zoom: 1 };
const node = (aspect?: number): CanvasNode => ({
  id: "n",
  type: "canvas",
  position: { x: 0, y: 0 },
  data: { kind: "image", label: "图片", src: "/files/a.png", assetId: "7", status: "done", aspect },
});

describe("图片节点画幅 aspect 的持久化", () => {
  test("量出来的比例存下来，重开画布时节点一开始就是对的高度，不再跳一下", () => {
    const saved = serializeGraph([node(1.98)], [], viewport);
    expect(saved.nodes[0]?.data).toMatchObject({ aspect: 1.98 });
    const [loaded] = deserializeGraph(saved).nodes;
    expect((loaded as CanvasNode).data.aspect).toBe(1.98);
  });

  test("没有量过就不写这个字段", () => {
    const saved = serializeGraph([node()], [], viewport);
    expect(saved.nodes[0]?.data).not.toHaveProperty("aspect");
  });
});
