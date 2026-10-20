import { describe, expect, test } from "bun:test";

import { canvasDialogGuards } from "@/utils/canvas/dialog-guards";

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

describe("canvasDialogGuards 画布里弹窗的事件守卫", () => {
  // 弹窗经 Portal 渲染到 body，但 React 事件按组件树冒泡，会一路冒到节点和画布上：
  // 双击会打开节点预览、在空白处新建节点，点击会选中节点、取消选中
  test.each(["onClick", "onDoubleClick", "onPointerDown", "onMouseDown", "onContextMenu"] as const)(
    "%s 被拦住，不冒泡到画布",
    (name) => {
      const fake = fakeEvent();
      canvasDialogGuards[name](fake.event);
      expect(fake.stopped()).toBe(1);
    },
  );
});
