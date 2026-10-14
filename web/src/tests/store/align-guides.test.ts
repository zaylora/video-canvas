import { beforeEach, describe, expect, test } from "bun:test";

import { useAlignGuidesStore } from "@/store/align-guides";
import type { Guide } from "@/utils/canvas/alignment";

const x: Guide = { axis: "x", value: 100, from: 0, to: 50 };
const y: Guide = { axis: "y", value: 20, from: 0, to: 80 };

beforeEach(() => useAlignGuidesStore.getState().clear());

describe("useAlignGuidesStore", () => {
  test("初始没有辅助线", () => {
    expect(useAlignGuidesStore.getState().guides).toEqual([]);
  });

  test("setGuides 写入", () => {
    useAlignGuidesStore.getState().setGuides([x, y]);
    expect(useAlignGuidesStore.getState().guides).toEqual([x, y]);
  });

  test("内容没变：不通知订阅者，也不换数组引用（拖动每帧调用，不能无谓重渲染）", () => {
    useAlignGuidesStore.getState().setGuides([x]);
    const before = useAlignGuidesStore.getState().guides;
    let notified = 0;
    const off = useAlignGuidesStore.subscribe(() => notified++);
    useAlignGuidesStore.getState().setGuides([{ ...x }]);
    off();
    expect(notified).toBe(0);
    expect(useAlignGuidesStore.getState().guides).toBe(before);
  });

  test("线的范围变了（跟着节点延伸）：要通知", () => {
    useAlignGuidesStore.getState().setGuides([x]);
    let notified = 0;
    const off = useAlignGuidesStore.subscribe(() => notified++);
    useAlignGuidesStore.getState().setGuides([{ ...x, to: 60 }]);
    off();
    expect(notified).toBe(1);
  });

  test("clear 清空；本来就是空的不通知", () => {
    useAlignGuidesStore.getState().setGuides([x]);
    useAlignGuidesStore.getState().clear();
    expect(useAlignGuidesStore.getState().guides).toEqual([]);
    let notified = 0;
    const off = useAlignGuidesStore.subscribe(() => notified++);
    useAlignGuidesStore.getState().clear();
    off();
    expect(notified).toBe(0);
  });
});
