import { describe, expect, test } from "bun:test";
import type { NodeChange } from "@xyflow/react";

import type { CanvasGroupNode, CanvasNode, FlowNode } from "@/types";
import { adjustForAlign, startAlignSession } from "@/utils/canvas/align-session";

const node = (id: string, x: number, y: number, extra: Partial<CanvasNode> = {}): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  measured: { width: 100, height: 60 },
  data: { kind: "image", label: id },
  ...extra,
});

const group = (id: string, x: number, y: number): CanvasGroupNode => ({
  id,
  type: "group",
  position: { x, y },
  width: 400,
  height: 300,
  data: { label: id },
});

const move = (
  id: string,
  x: number,
  y: number,
  dragging = true,
  absolute?: { x: number; y: number },
): NodeChange<FlowNode> => ({
  id,
  type: "position",
  position: { x, y },
  positionAbsolute: absolute,
  dragging,
});

const positionOf = (changes: NodeChange<FlowNode>[], id = "a") => {
  const found = changes.find((change) => change.type === "position" && change.id === id);
  return found?.type === "position" ? found : undefined;
};

describe("startAlignSession 什么时候介入", () => {
  const nodes: FlowNode[] = [node("a", 0, 0), node("b", 300, 0)];

  test("单个普通节点：建会话", () => {
    expect(startAlignSession(nodes, [nodes[0]])).not.toBeNull();
  });

  test("多选拖动：不介入", () => {
    expect(startAlignSession(nodes, nodes)).toBeNull();
  });

  test("拖的是组：不介入", () => {
    const withGroup = [group("g", 0, 0), ...nodes];
    expect(startAlignSession(withGroup, [withGroup[0]])).toBeNull();
  });

  test("被拖节点还没测量出尺寸：不介入，不把未知尺寸当 0", () => {
    const unmeasured = node("u", 0, 0, { measured: undefined });
    expect(startAlignSession([unmeasured, nodes[1]], [unmeasured])).toBeNull();
  });
});

describe("startAlignSession 谁是目标", () => {
  test("排除自己、组、隐藏的、没测量的", () => {
    const nodes: FlowNode[] = [
      group("g", 0, 0),
      node("a", 0, 0),
      node("ok", 300, 0),
      node("hidden", 500, 0, { hidden: true }),
      node("unmeasured", 700, 0, { measured: undefined }),
    ];
    const session = startAlignSession(nodes, [nodes[1]]);
    expect(session?.targets.map((target) => target.id)).toEqual(["ok"]);
  });

  test("组成员的矩形换算成画布绝对坐标", () => {
    const nodes: FlowNode[] = [
      group("g", 100, 100),
      node("member", 20, 30, { parentId: "g" }),
      node("a", 0, 0),
    ];
    const session = startAlignSession(nodes, [nodes[2]]);
    expect(session?.targets[0].rect).toEqual({ x: 120, y: 130, width: 100, height: 60 });
  });

  test("父组不存在时按原位置算，不报错", () => {
    const nodes: FlowNode[] = [node("orphan", 20, 30, { parentId: "gone" }), node("a", 0, 0)];
    expect(startAlignSession(nodes, [nodes[1]])?.targets[0].rect.x).toBe(20);
  });
});

describe("adjustForAlign 改写位置变更", () => {
  const nodes: FlowNode[] = [node("a", 0, 0), node("b", 300, 200)];
  const fresh = () => startAlignSession(nodes, [nodes[0]])!;

  test("靠近目标：位置被拉过去，position 和 positionAbsolute 同步修正", () => {
    const out = adjustForAlign(fresh(), [move("a", 298, 120, true, { x: 298, y: 120 })], 1);
    const change = positionOf(out.changes);
    expect(change?.position).toEqual({ x: 300, y: 120 });
    expect(change?.positionAbsolute).toEqual({ x: 300, y: 120 });
  });

  test("左中右都对齐时画三条竖线，只连在两个节点之间的空隙", () => {
    const out = adjustForAlign(fresh(), [move("a", 298, 120)], 1);
    // 吸附后 a 在 (300,120)，竖直方向 a 占 120–180，b 占 200–260，空隙 180–200
    expect(out.guides?.map((guide) => [guide.axis, guide.value, guide.from, guide.to])).toEqual([
      ["x", 300, 180, 200],
      ["x", 350, 180, 200],
      ["x", 400, 180, 200],
    ]);
  });

  test("两个轴同时吸附：横竖两个方向都有线", () => {
    const out = adjustForAlign(fresh(), [move("a", 298, 197)], 1);
    expect(positionOf(out.changes)?.position).toEqual({ x: 300, y: 200 });
    expect(new Set(out.guides?.map((guide) => guide.axis))).toEqual(new Set(["x", "y"]));
  });

  test("辅助线按吸附之后的位置算，而不是原始位置", () => {
    // y 方向先吸到 b 的上边（200），x 方向再吸：竖线的空隙应该用吸附后的 y（200–260 与 b 重叠）
    const out = adjustForAlign(fresh(), [move("a", 298, 197)], 1);
    const vertical = out.guides?.find((guide) => guide.axis === "x");
    expect([vertical?.from, vertical?.to]).toEqual([200, 260]);
  });

  test("离得远：位置原样，没有辅助线", () => {
    const changes = [move("a", 100, 100)];
    const out = adjustForAlign(fresh(), changes, 1);
    expect(out.changes).toEqual(changes);
    expect(out.guides).toEqual([]);
  });

  test("没有被拖节点的位置变更（选中、尺寸）：原样返回，辅助线不更新", () => {
    const changes: NodeChange<FlowNode>[] = [{ id: "a", type: "select", selected: true }];
    const out = adjustForAlign(fresh(), changes, 1);
    expect(out.changes).toBe(changes);
    expect(out.guides).toBeNull();
  });

  test("别的节点的位置变更不动", () => {
    const changes = [move("b", 298, 120)];
    expect(adjustForAlign(fresh(), changes, 1).changes).toEqual(changes);
  });

  test("组成员：position 是相对坐标，吸附按绝对坐标算，修正量对两种坐标相同", () => {
    const members: FlowNode[] = [
      group("g", 100, 100),
      node("m", 20, 20, { parentId: "g" }),
      node("t", 300, 0),
    ];
    const session = startAlignSession(members, [members[1]])!;
    // 成员起点绝对 (120,120)；相对 x=178 → 绝对左边 278，离目标 22，不吸
    expect(
      positionOf(adjustForAlign(session, [move("m", 178, 20)], 1).changes, "m")?.position,
    ).toEqual({
      x: 178,
      y: 20,
    });
    // 相对 x=198 → 绝对 298，离目标左边 300 差 2：吸，相对坐标修正成 200
    const out = adjustForAlign(session, [move("m", 198, 20)], 1);
    expect(positionOf(out.changes, "m")?.position).toEqual({ x: 200, y: 20 });
    expect(out.guides?.[0].value).toBe(300);
  });
});

describe("adjustForAlign 黏性与缩放", () => {
  const nodes: FlowNode[] = [node("a", 0, 0), node("b", 300, 200)];
  const fresh = () => startAlignSession(nodes, [nodes[0]])!;

  test("吸住后在释放距离内继续保持，拖出去才松开", () => {
    const session = fresh();
    adjustForAlign(session, [move("a", 298, 0)], 1);
    expect(positionOf(adjustForAlign(session, [move("a", 305, 0)], 1).changes)?.position.x).toBe(
      300,
    );
    expect(positionOf(adjustForAlign(session, [move("a", 309, 0)], 1).changes)?.position.x).toBe(
      309,
    );
  });

  test("缩小到 0.5：进入距离折算成 8 画布单位，差 6 也吸", () => {
    const out = adjustForAlign(fresh(), [move("a", 294, 0)], 0.5);
    expect(positionOf(out.changes)?.position.x).toBe(300);
  });

  test("放大到 2：进入距离折算成 2 画布单位，差 2 不吸", () => {
    const out = adjustForAlign(fresh(), [move("a", 298, 0)], 2);
    expect(positionOf(out.changes)?.position.x).toBe(298);
  });

  test("缩放值异常（0）时不除零", () => {
    expect(() => adjustForAlign(fresh(), [move("a", 298, 0)], 0)).not.toThrow();
  });
});

describe("adjustForAlign 松手那一帧", () => {
  const nodes: FlowNode[] = [node("a", 0, 0), node("b", 300, 200)];

  test("xyflow 松手发的是原始坐标：复用最后一次修正量，节点不跳回", () => {
    const session = startAlignSession(nodes, [nodes[0]])!;
    adjustForAlign(session, [move("a", 298, 0)], 1);
    const out = adjustForAlign(session, [move("a", 298, 0, false)], 1);
    expect(positionOf(out.changes)?.position).toEqual({ x: 300, y: 0 });
    expect(positionOf(out.changes)?.dragging).toBe(false);
    expect(out.guides).toEqual([]);
  });

  test("最后一帧没有吸附：松手帧原样", () => {
    const session = startAlignSession(nodes, [nodes[0]])!;
    adjustForAlign(session, [move("a", 100, 0)], 1);
    const changes = [move("a", 100, 0, false)];
    expect(adjustForAlign(session, changes, 1).changes).toEqual(changes);
  });
});
