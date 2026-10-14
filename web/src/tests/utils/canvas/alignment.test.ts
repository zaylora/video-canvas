import { describe, expect, test } from "bun:test";

import {
  ENTER_PX,
  RELEASE_PX,
  alignmentGuides,
  anchorValue,
  findAxisSnap,
  type Rect,
  type SnapTarget,
} from "@/utils/canvas/alignment";

const rect = (x: number, y: number, width = 100, height = 60): Rect => ({ x, y, width, height });
const target = (id: string, r: Rect): SnapTarget => ({ id, rect: r });

describe("anchorValue", () => {
  const r = rect(10, 20, 100, 60);

  test("x 轴：左、中、右", () => {
    expect([0, 1, 2].map((i) => anchorValue(r, "x", i as 0 | 1 | 2))).toEqual([10, 60, 110]);
  });

  test("y 轴：上、中、下", () => {
    expect([0, 1, 2].map((i) => anchorValue(r, "y", i as 0 | 1 | 2))).toEqual([20, 50, 80]);
  });
});

describe("阈值常量", () => {
  test("进入比释放小：吸住之后要拖得更远才松开，边界不抖", () => {
    expect(ENTER_PX).toBe(4);
    expect(RELEASE_PX).toBe(8);
  });
});

describe("findAxisSnap 进入吸附", () => {
  const t = target("t", rect(200, 100));

  test("没有目标返回 null", () => {
    expect(findAxisSnap(rect(0, 0), [], "x", 4, 8)).toBeNull();
  });

  test("左边靠近目标左边：返回把它拉过去的修正量", () => {
    const snap = findAxisSnap(rect(197, 0), [t], "x", 4, 8);
    expect(snap?.delta).toBe(3);
    expect(snap?.lock.value).toBe(200);
  });

  test("距离恰好等于进入距离不吸（严格小于）", () => {
    expect(findAxisSnap(rect(196, 0), [t], "x", 4, 8)).toBeNull();
  });

  test("距离大于进入距离不吸", () => {
    expect(findAxisSnap(rect(120, 0), [t], "x", 4, 8)).toBeNull();
  });

  test("中心对中心", () => {
    // 被拖 x 198–302，中心正好是 250，和目标中心重合；左右边各差 2，中心差 0 最近
    const moving = rect(198, 0, 104, 60);
    const snap = findAxisSnap(moving, [target("t", rect(200, 100, 100, 60))], "x", 4, 8);
    expect(snap?.delta).toBe(0);
    expect(snap?.lock.value).toBe(250);
  });

  test("交叉锚点：被拖的右边对齐目标的左边", () => {
    // 被拖右边 = 98 + 100 = 198，目标左边 = 200
    const snap = findAxisSnap(rect(98, 0), [t], "x", 4, 8);
    expect(snap?.delta).toBe(2);
    expect(snap?.lock.value).toBe(200);
  });

  test("y 轴同样适用：上边靠近目标下边", () => {
    // 目标下边 = 160；被拖上边 = 158
    const snap = findAxisSnap(rect(0, 158), [t], "y", 4, 8);
    expect(snap?.delta).toBe(2);
    expect(snap?.lock.value).toBe(160);
  });

  test("多个候选时取距离最近的", () => {
    const near = target("near", rect(202, 0));
    const far = target("far", rect(204, 0));
    const snap = findAxisSnap(rect(200, 0), [far, near], "x", 6, 12);
    expect(snap?.lock.targetId).toBe("near");
    expect(snap?.delta).toBe(2);
  });

  test("距离相同时取先出现的目标，结果是确定的", () => {
    const a = target("a", rect(203, 0));
    const b = target("b", rect(197, 0));
    expect(findAxisSnap(rect(200, 0), [a, b], "x", 6, 12)?.lock.targetId).toBe("a");
    expect(findAxisSnap(rect(200, 0), [b, a], "x", 6, 12)?.lock.targetId).toBe("b");
  });
});

describe("findAxisSnap 释放与黏性", () => {
  const t = target("t", rect(200, 100));
  const first = findAxisSnap(rect(198, 0), [t], "x", 4, 8)!;

  test("已吸住时，在释放距离内继续保持", () => {
    // 原始位置漂到 205：离进入距离已远，但仍在释放距离 8 内
    const snap = findAxisSnap(rect(205, 0), [t], "x", 4, 8, first.lock);
    expect(snap?.delta).toBe(-5);
    expect(snap?.lock).toEqual(first.lock);
  });

  test("拖出释放距离后松开；没有别的目标时返回 null", () => {
    expect(findAxisSnap(rect(209, 0), [t], "x", 4, 8, first.lock)).toBeNull();
  });

  test("距离恰好等于释放距离仍保持（小于等于）", () => {
    expect(findAxisSnap(rect(208, 0), [t], "x", 4, 8, first.lock)?.delta).toBe(-8);
  });

  test("锁定的目标已经不在了，当作没有锁", () => {
    expect(findAxisSnap(rect(205, 0), [], "x", 4, 8, first.lock)).toBeNull();
  });

  test("释放后如果旁边有新的目标在进入距离内，直接吸到新目标", () => {
    const other = target("other", rect(212, 0));
    const snap = findAxisSnap(rect(210, 0), [t, other], "x", 4, 8, first.lock);
    expect(snap?.lock.targetId).toBe("other");
  });
});

describe("alignmentGuides 辅助线只连两个节点之间的空隙", () => {
  test("左右并排、上中下三条边都对齐：画三条横线，只在两个节点之间", () => {
    const guides = alignmentGuides(rect(0, 0, 100, 60), rect(200, 0, 100, 60), "y");
    expect(guides).toEqual([
      { axis: "y", value: 0, from: 100, to: 200 },
      { axis: "y", value: 30, from: 100, to: 200 },
      { axis: "y", value: 60, from: 100, to: 200 },
    ]);
  });

  test("只有一对对齐：只画一条", () => {
    const guides = alignmentGuides(rect(0, 0, 100, 60), rect(200, 0, 100, 40), "y");
    expect(guides).toEqual([{ axis: "y", value: 0, from: 100, to: 200 }]);
  });

  test("上下堆叠、左中右对齐：竖线连在上下两个节点之间", () => {
    const guides = alignmentGuides(rect(0, 0, 100, 60), rect(0, 100, 100, 60), "x");
    expect(guides.map((guide) => [guide.value, guide.from, guide.to])).toEqual([
      [0, 60, 100],
      [50, 60, 100],
      [100, 60, 100],
    ]);
  });

  test("被拖节点在目标另一侧：空隙从目标的终点到被拖节点的起点", () => {
    const guides = alignmentGuides(rect(300, 0, 100, 60), rect(0, 0, 100, 60), "y");
    expect(guides[0]).toEqual({ axis: "y", value: 0, from: 100, to: 300 });
  });

  test("两个节点在另一轴上有重叠：线画在重叠的那一段", () => {
    const guides = alignmentGuides(rect(0, 0, 100, 60), rect(60, 0, 100, 60), "y");
    expect(guides[0]).toEqual({ axis: "y", value: 0, from: 60, to: 100 });
  });

  test("刚好贴着、没有空隙：不画线", () => {
    expect(alignmentGuides(rect(0, 0, 100, 60), rect(100, 0, 100, 60), "y")).toEqual([]);
  });

  test("没有对齐的锚点：不画线", () => {
    expect(alignmentGuides(rect(0, 0, 100, 60), rect(200, 17, 100, 31), "y")).toEqual([]);
  });

  test("半个像素以内的误差算对齐，超过就不算", () => {
    expect(alignmentGuides(rect(0, 0.3, 100, 60), rect(200, 0, 100, 60), "y")).toHaveLength(3);
    expect(alignmentGuides(rect(0, 1, 100, 60), rect(200, 0, 100, 60), "y")).toHaveLength(0);
  });
});
