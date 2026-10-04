import { describe, expect, test } from "bun:test";

import type { CanvasGroupNode, CanvasNode, FlowNode } from "@/types";
import {
  GROUP_MIN,
  GROUP_TITLE_MAX_SCALE,
  canResizeGroup,
  GROUP_Z_INDEX,
  GROUP_PADDING,
  absolutePosition,
  createGroup,
  fitGroupToMembers,
  groupMembers,
  isGroupNode,
  localizePositions,
  mergeContentNodes,
  membersBounds,
  nextGroupLabel,
  normalizeFlowNodes,
  removeGroupWithMembers,
  resolveParent,
  groupTitleScale,
  setParent,
  ungroup,
} from "@/utils/canvas/group";

const node = (id: string, x: number, y: number, width = 100, height = 50): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  measured: { width, height },
  data: { kind: "image", label: id },
});

const group = (
  id: string,
  x: number,
  y: number,
  width: number,
  height: number,
): CanvasGroupNode => ({
  id,
  type: "group",
  position: { x, y },
  width,
  height,
  data: { label: id },
});

const byId = (nodes: FlowNode[], id: string) => nodes.find((n) => n.id === id) as FlowNode;

describe("isGroupNode", () => {
  test("按 type 区分组和普通节点", () => {
    expect(isGroupNode(group("g", 0, 0, 10, 10))).toBe(true);
    expect(isGroupNode(node("a", 0, 0))).toBe(false);
  });
});

describe("createGroup", () => {
  const nodes: FlowNode[] = [node("a", 100, 200), node("b", 400, 260), node("c", 150, 210)];

  test("组框 = 选中节点包围盒加留白，只有被选中的节点成为成员", () => {
    const { nodes: out, group: g } = createGroup(nodes, ["a", "b"], "组");
    expect(g.position).toEqual({
      x: 100 - GROUP_PADDING.x,
      y: 200 - GROUP_PADDING.top,
    });
    expect(g.width).toBe(400 + 100 - 100 + GROUP_PADDING.x * 2);
    expect(g.height).toBe(260 + 50 - 200 + GROUP_PADDING.top + GROUP_PADDING.bottom);
    expect(byId(out, "a").parentId).toBe(g.id);
    expect(byId(out, "b").parentId).toBe(g.id);
    // c 虽然落在组框里，但没被选中，不会被带进来
    expect(byId(out, "c").parentId).toBeUndefined();
  });

  test("成员改存相对组左上角的坐标，视觉位置不变", () => {
    const { nodes: out, group: g } = createGroup(nodes, ["a", "b"], "组");
    const a = byId(out, "a");
    expect(a.position).toEqual({ x: GROUP_PADDING.x, y: GROUP_PADDING.top });
    expect(absolutePosition(a, out)).toEqual({ x: 100, y: 200 });
    expect(g.data.label).toBe("组");
  });

  test("组排在所有成员之前（xyflow 要求父节点在前）", () => {
    const { nodes: out, group: g } = createGroup(nodes, ["a", "b"], "组");
    const gi = out.findIndex((n) => n.id === g.id);
    for (const id of ["a", "b"]) expect(gi).toBeLessThan(out.findIndex((n) => n.id === id));
  });

  test("已在别的组里的节点会被移进新组，坐标按绝对位置换算", () => {
    const old = group("old", 0, 0, 800, 600);
    const inner = { ...node("a", 100, 100), parentId: "old" };
    const start: FlowNode[] = [old, inner, node("b", 300, 300)];
    const { nodes: out, group: g } = createGroup(start, ["a", "b"], "新组");
    expect(byId(out, "a").parentId).toBe(g.id);
    expect(absolutePosition(byId(out, "a"), out)).toEqual({ x: 100, y: 100 });
  });

  test("选中不足 2 个普通节点时不建组", () => {
    expect(createGroup(nodes, ["a"], "组").group).toBeNull();
    expect(createGroup(nodes, ["nope", "a"], "组").group).toBeNull();
  });

  test("选中的组不会被打进新组（组不嵌套）", () => {
    const start: FlowNode[] = [group("g1", 0, 0, 300, 300), node("a", 10, 10), node("b", 500, 10)];
    const { group: g } = createGroup(start, ["g1", "a", "b"], "组");
    expect(g).not.toBeNull();
  });
});

describe("resolveParent", () => {
  const g = group("g", 0, 0, 400, 300);

  test("节点中心点在组框内就属于这个组", () => {
    const n = node("a", 330, 100); // 中心 (380, 125) 在框内，虽然右边缘已出框
    expect(resolveParent(n, [g, n])).toBe("g");
  });

  test("中心点出框就不属于，哪怕大半个节点还压着组框", () => {
    const n = node("a", 360, 100); // 中心 (410, 125) 出框
    expect(resolveParent(n, [g, n])).toBeUndefined();
  });

  test("成员用相对坐标，判定要先换算成绝对位置", () => {
    const m = { ...node("a", 50, 50), parentId: "g" };
    const moved = group("g", 1000, 1000, 400, 300);
    expect(resolveParent(m, [moved, m])).toBe("g");
    const far = { ...m, position: { x: 900, y: 50 } };
    expect(resolveParent(far, [moved, far])).toBeUndefined();
  });

  test("重叠的两个组：后面（更上层）的优先", () => {
    const g2 = group("g2", 0, 0, 400, 300);
    const n = node("a", 100, 100);
    expect(resolveParent(n, [g, g2, n])).toBe("g2");
  });

  test("可以跳过正在被拖动的组", () => {
    const n = node("a", 100, 100);
    expect(resolveParent(n, [g, n], new Set(["g"]))).toBeUndefined();
  });
});

describe("setParent", () => {
  const g = group("g", 500, 500, 400, 300);

  test("加入组：绝对位置不变，改成相对坐标", () => {
    const n = node("a", 600, 620);
    const out = setParent([g, n], "a", "g");
    const a = byId(out, "a");
    expect(a.parentId).toBe("g");
    expect(a.position).toEqual({ x: 100, y: 120 });
  });

  test("退出组：换回绝对坐标并去掉 parentId", () => {
    const m = { ...node("a", 100, 120), parentId: "g" };
    const out = setParent([g, m], "a", undefined);
    const a = byId(out, "a");
    expect(a.parentId).toBeUndefined();
    expect(a.position).toEqual({ x: 600, y: 620 });
  });

  test("父没变就原样返回", () => {
    const nodes: FlowNode[] = [g, { ...node("a", 100, 120), parentId: "g" }];
    expect(setParent(nodes, "a", "g")).toBe(nodes);
  });

  test("加入后组仍排在成员之前", () => {
    const out = setParent([node("a", 600, 620), g], "a", "g");
    expect(out.findIndex((n) => n.id === "g")).toBeLessThan(out.findIndex((n) => n.id === "a"));
  });
});

describe("ungroup", () => {
  test("组消失，成员原地保留（回到绝对坐标）", () => {
    const g = group("g", 500, 500, 400, 300);
    const m = { ...node("a", 100, 120), parentId: "g" };
    const out = ungroup([g, m, node("b", 0, 0)], "g");
    expect(out.map((n) => n.id)).toEqual(["a", "b"]);
    expect(byId(out, "a").position).toEqual({ x: 600, y: 620 });
    expect(byId(out, "a").parentId).toBeUndefined();
  });
});

describe("removeGroupWithMembers", () => {
  test("返回组和全部成员的 id", () => {
    const g = group("g", 0, 0, 400, 300);
    const nodes: FlowNode[] = [g, { ...node("a", 1, 1), parentId: "g" }, node("b", 0, 0)];
    expect(removeGroupWithMembers(nodes, "g").sort()).toEqual(["a", "g"]);
  });
});

describe("成员与包围盒", () => {
  test("groupMembers 只取 parentId 指向该组的节点", () => {
    const g = group("g", 0, 0, 400, 300);
    const nodes: FlowNode[] = [g, { ...node("a", 1, 1), parentId: "g" }, node("b", 0, 0)];
    expect(groupMembers(nodes, "g").map((n) => n.id)).toEqual(["a"]);
  });

  test("membersBounds 给出成员相对组的包围盒，没有成员返回 null", () => {
    const g = group("g", 0, 0, 400, 300);
    const nodes: FlowNode[] = [
      g,
      { ...node("a", 20, 30), parentId: "g" },
      { ...node("b", 200, 100, 50, 40), parentId: "g" },
    ];
    expect(membersBounds(nodes, "g")).toEqual({ x0: 20, y0: 30, x1: 250, y1: 140 });
    expect(membersBounds([g], "g")).toBeNull();
  });
});

describe("fitGroupToMembers", () => {
  test("组框贴合成员（含留白），成员的视觉位置不变", () => {
    const g = group("g", 0, 0, 900, 900);
    const nodes: FlowNode[] = [
      g,
      { ...node("a", 300, 400), parentId: "g" },
      { ...node("b", 500, 600), parentId: "g" },
    ];
    const out = fitGroupToMembers(nodes, "g");
    const fitted = byId(out, "g") as CanvasGroupNode;
    expect(fitted.position).toEqual({ x: 300 - GROUP_PADDING.x, y: 400 - GROUP_PADDING.top });
    expect(fitted.width).toBe(300 + GROUP_PADDING.x * 2);
    expect(fitted.height).toBe(250 + GROUP_PADDING.top + GROUP_PADDING.bottom);
    expect(absolutePosition(byId(out, "a"), out)).toEqual({ x: 300, y: 400 });
    expect(absolutePosition(byId(out, "b"), out)).toEqual({ x: 500, y: 600 });
  });

  test("不小于最小尺寸", () => {
    const g = group("g", 0, 0, 900, 900);
    const nodes: FlowNode[] = [g, { ...node("a", 300, 400, 20, 20), parentId: "g" }];
    const fitted = byId(fitGroupToMembers(nodes, "g"), "g") as CanvasGroupNode;
    expect(fitted.width).toBeGreaterThanOrEqual(GROUP_MIN.width);
    expect(fitted.height).toBeGreaterThanOrEqual(GROUP_MIN.height);
  });
});

describe("normalizeFlowNodes（读档时修整）", () => {
  test("组排到成员前面，其余相对顺序不变", () => {
    const nodes: FlowNode[] = [
      { ...node("a", 1, 1), parentId: "g" },
      node("b", 0, 0),
      group("g", 0, 0, 400, 300),
    ];
    expect(normalizeFlowNodes(nodes).map((n) => n.id)).toEqual(["g", "a", "b"]);
  });

  test("parentId 指向不存在的组：清掉并保持原位置", () => {
    const out = normalizeFlowNodes([{ ...node("a", 7, 8), parentId: "ghost" }]);
    expect(out[0].parentId).toBeUndefined();
    expect(out[0].position).toEqual({ x: 7, y: 8 });
  });

  test("parentId 指向的不是组：同样清掉", () => {
    const out = normalizeFlowNodes([node("p", 0, 0), { ...node("a", 7, 8), parentId: "p" }]);
    expect(byId(out, "a").parentId).toBeUndefined();
  });
});

describe("nextGroupLabel", () => {
  test("「组」被占了依次叫「组 2」「组 3」", () => {
    expect(nextGroupLabel([])).toBe("组");
    expect(
      nextGroupLabel([group("a", 0, 0, 1, 1)].map((g) => ({ ...g, data: { label: "组" } }))),
    ).toBe("组 2");
    const taken = ["组", "组 2"].map((label, i) => ({
      ...group(`g${i}`, 0, 0, 1, 1),
      data: { label },
    }));
    expect(nextGroupLabel(taken)).toBe("组 3");
  });
});

describe("mergeContentNodes", () => {
  test("只换掉普通节点，组原样保留且仍在最前", () => {
    const g = group("g", 0, 0, 400, 300);
    const prev: FlowNode[] = [g, { ...node("a", 1, 1), parentId: "g" }, node("b", 5, 5)];
    const out = mergeContentNodes(prev, [{ ...node("a", 9, 9), parentId: "g" }, node("c", 0, 0)]);
    expect(out.map((n) => n.id)).toEqual(["g", "a", "c"]);
    expect(out[0]).toBe(g);
    expect(byId(out, "a").position).toEqual({ x: 9, y: 9 });
  });
});

describe("层级", () => {
  test("新建的组和读档的组都垫在所有节点下面（选中时 xyflow 会再加 1000，仍要低于 0）", () => {
    const { group: g } = createGroup([node("a", 0, 0), node("b", 200, 0)], ["a", "b"], "组");
    expect(g?.zIndex).toBe(GROUP_Z_INDEX);
    expect(GROUP_Z_INDEX + 1000).toBeLessThan(0);
    const loaded = normalizeFlowNodes([group("g", 0, 0, 300, 300)]);
    expect(loaded[0].zIndex).toBe(GROUP_Z_INDEX);
  });
});

describe("localizePositions", () => {
  test("把一批绝对目标位置换回各节点自己的坐标系（成员相对组，其余绝对）", () => {
    const g = group("g", 100, 100, 600, 400);
    const member = { ...node("a", 10, 10), parentId: "g" };
    const loose = node("b", 900, 900);
    const out = localizePositions(
      [g, member, loose],
      new Map([
        ["a", { x: 300, y: 250 }],
        ["b", { x: 50, y: 60 }],
      ]),
    );
    expect(out.get("a")).toEqual({ x: 200, y: 150 });
    expect(out.get("b")).toEqual({ x: 50, y: 60 });
  });
});

describe("groupTitleScale", () => {
  test("正常和放大时不缩放", () => {
    expect(groupTitleScale(1)).toBe(1);
    expect(groupTitleScale(2.5)).toBe(1);
  });

  test("缩小画布时反向放大，保持屏幕上的字号可读", () => {
    expect(groupTitleScale(0.5)).toBe(2);
    expect(groupTitleScale(0.25)).toBe(4);
  });

  test("放大有上限，缩到很小也不会无限放大", () => {
    expect(groupTitleScale(0.14)).toBe(GROUP_TITLE_MAX_SCALE);
    expect(groupTitleScale(0.01)).toBe(GROUP_TITLE_MAX_SCALE);
  });

  test("缩放值异常（0、负数、NaN）时按不缩放处理", () => {
    expect(groupTitleScale(0)).toBe(1);
    expect(groupTitleScale(-1)).toBe(1);
    expect(groupTitleScale(Number.NaN)).toBe(1);
  });
});

describe("canResizeGroup", () => {
  // 组框 (0,0)-(1000,800)，成员包围盒（绝对坐标）(100,150)-(900,700)
  const current = { x: 0, y: 0, width: 1000, height: 800 };
  const members = { x0: 100, y0: 150, x1: 900, y1: 700 };
  const frame = (x: number, y: number, right: number, bottom: number) => ({
    x,
    y,
    width: right - x,
    height: bottom - y,
  });

  test("放大随便放", () => {
    expect(canResizeGroup(current, frame(-200, -200, 1300, 1100), members)).toBe(true);
  });

  test("可以缩小，只要还罩得住成员（留一点边距）", () => {
    expect(canResizeGroup(current, frame(60, 60, 940, 740), members)).toBe(true);
  });

  test("缩到压住成员就不行，四条边各自判断", () => {
    expect(canResizeGroup(current, frame(120, 0, 1000, 800), members)).toBe(false); // 左边越过成员
    expect(canResizeGroup(current, frame(0, 140, 1000, 800), members)).toBe(false); // 上边越过成员
    expect(canResizeGroup(current, frame(0, 0, 880, 800), members)).toBe(false); // 右边越过成员
    expect(canResizeGroup(current, frame(0, 0, 1000, 690), members)).toBe(false); // 下边越过成员
  });

  test("上方要给节点标题行留位置：上边不能缩到离成员顶部不足标题高度", () => {
    expect(canResizeGroup(current, frame(0, 130, 1000, 800), members)).toBe(false);
  });

  test("老组留白不够（已经偏紧）时仍能往外拉，只是不能再往里收", () => {
    const tight = { x: 90, y: 140, width: 820, height: 570 }; // 比成员只大一点点
    expect(canResizeGroup(tight, frame(40, 100, 960, 800), members)).toBe(true); // 向外拉
    expect(canResizeGroup(tight, frame(120, 140, 910, 710), members)).toBe(false); // 向内收
  });

  test("没有成员时只受调用方的最小尺寸约束，永远放行", () => {
    expect(canResizeGroup(current, frame(0, 0, 10, 10), null)).toBe(true);
  });
});
