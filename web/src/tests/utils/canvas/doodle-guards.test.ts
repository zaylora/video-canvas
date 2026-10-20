import { describe, expect, test } from "bun:test";

import { doodleStageGuards } from "@/utils/canvas/doodle-guards";

/** 只带 stopPropagation 的假事件，数一数被拦了几次 */
function fakeEvent() {
  let stopped = 0;
  return {
    event: {
      stopPropagation: () => {
        stopped += 1;
      },
    } as never,
    stopped: () => stopped,
  };
}

describe("doodleStageGuards 涂鸦画布的手势守卫", () => {
  test("按下、点击、双击、右键、滚轮都拦住，不让画布拖动、框选、点空白取消选中", () => {
    for (const name of [
      "onPointerDown",
      "onMouseDown",
      "onClick",
      "onDoubleClick",
      "onContextMenu",
      "onWheel",
    ] as const) {
      const fake = fakeEvent();
      doodleStageGuards[name](fake.event);
      expect(fake.stopped()).toBe(1);
    }
  });

  test("不拦松开和移动：Fabric 按下后把 mouseup / mousemove 挂在 document 上，", () => {
    // React 的 stopPropagation 会让事件停在根节点，到不了 document，Fabric 收不到松开，笔迹就一直画下去
    const names = Object.keys(doodleStageGuards);
    for (const name of [
      "onMouseUp",
      "onMouseMove",
      "onPointerUp",
      "onPointerMove",
      "onPointerCancel",
    ]) {
      expect(names).not.toContain(name);
    }
  });
});
