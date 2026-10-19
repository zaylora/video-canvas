import { beforeEach, describe, expect, test } from "bun:test";

import { useDirectorStore } from "@/store/director";
import type { DirectorScene } from "@/types";
import { createDefaultScene } from "@/utils/director/scene";

const scene = (): DirectorScene => {
  let n = 0;
  return createDefaultScene(() => `id${++n}`);
};

/** 默认场景里的人物 id1、机位 id2 */
const CHAR = "id1";
const CAM = "id2";

const store = () => useDirectorStore.getState();

beforeEach(() => {
  store().reset();
  store().open("node1", scene());
});

describe("打开与关闭", () => {
  test("打开时带上场景，播放头在 0、没有选中", () => {
    const state = store();
    expect(state.nodeId).toBe("node1");
    expect(state.scene?.characters).toHaveLength(1);
    expect(state.t).toBe(0);
    expect(state.selectedId).toBeNull();
    expect(state.past).toHaveLength(0);
  });

  test("打开时可以要求直接弹导出，取走一次就清掉", () => {
    store().open("node1", scene(), { exportOnOpen: true });
    expect(store().takeExportOnOpen()).toBe(true);
    expect(store().takeExportOnOpen()).toBe(false);
  });

  test("导出中不能关闭", () => {
    store().setExporting(true);
    expect(store().close()).toBe(false);
    expect(store().nodeId).toBe("node1");
    store().setExporting(false);
    expect(store().close()).toBe(true);
    expect(store().nodeId).toBeNull();
    expect(store().scene).toBeNull();
  });
});

describe("撤销栈（编辑器独立）", () => {
  test("每次编辑算一步，可撤销、重做", () => {
    store().addObject("prop");
    store().addObject("prop");
    expect(store().scene?.props).toHaveLength(2);
    store().undo();
    expect(store().scene?.props).toHaveLength(1);
    store().redo();
    expect(store().scene?.props).toHaveLength(2);
  });

  test("新编辑会清掉重做", () => {
    store().addObject("prop");
    store().undo();
    store().addObject("character");
    expect(store().future).toHaveLength(0);
  });

  test("最多记 100 步", () => {
    for (let i = 0; i < 105; i++) store().setDuration(i % 2 ? 4 : 6);
    expect(store().past).toHaveLength(100);
  });

  test("拖动过程不入栈，松手算一步", () => {
    store().startGesture();
    store().transformObject(CHAR, { x: 1 });
    store().transformObject(CHAR, { x: 2 });
    expect(store().past).toHaveLength(0);
    store().endGesture();
    expect(store().past).toHaveLength(1);
    store().undo();
    expect(store().scene?.characters[0].x).toBe(0);
  });

  test("拖了但没动，不算一步", () => {
    store().startGesture();
    store().endGesture();
    expect(store().past).toHaveLength(0);
  });

  test("切当前机位不进撤销栈", () => {
    store().addObject("camera");
    const before = store().past.length;
    store().select(CAM);
    expect(store().scene?.activeCameraId).toBe(CAM);
    expect(store().past).toHaveLength(before);
  });
});

describe("添加对象", () => {
  test("新对象被选中；新机位成为当前机位", () => {
    store().addObject("camera");
    const cameras = store().scene!.cameras;
    expect(store().selectedId).toBe(cameras[1].id);
    expect(store().scene?.activeCameraId).toBe(cameras[1].id);
  });

  test("到上限返回提示，不改场景", () => {
    for (let i = 0; i < 5; i++) store().addObject("camera");
    expect(store().addObject("camera")).toContain("最多 6 个机位");
    expect(store().scene?.cameras).toHaveLength(6);
  });

  test("删除选中的对象后取消选中", () => {
    store().select(CHAR);
    store().removeObject(CHAR);
    expect(store().selectedId).toBeNull();
    expect(store().scene?.characters).toHaveLength(0);
  });
});

describe("变换与临时姿态", () => {
  test("没有关键帧：直接改静态站位", () => {
    store().transformObject(CHAR, { x: 3, yaw: 90 });
    expect(store().scene?.characters[0]).toMatchObject({ x: 3, yaw: 90 });
    expect(store().pending[CHAR]).toBeUndefined();
  });

  test("已有关键帧：改动只是临时姿态，不进轨道", () => {
    store().recordKey(CHAR);
    store().setTime(2);
    store().transformObject(CHAR, { x: 3 });
    expect(store().pending[CHAR]).toMatchObject({ x: 3 });
    expect(store().scene?.characters[0].keys).toHaveLength(1);
    expect(store().poseOf(CHAR)).toMatchObject({ x: 3 });
  });

  test("移动播放头丢弃临时姿态", () => {
    store().recordKey(CHAR);
    store().transformObject(CHAR, { x: 3 });
    store().setTime(1);
    expect(store().pending[CHAR]).toBeUndefined();
  });

  test("按 K 把临时姿态写进轨道", () => {
    store().recordKey(CHAR);
    store().setTime(2);
    store().transformObject(CHAR, { x: 3 });
    store().recordKey(CHAR);
    const keys = store().scene!.characters[0].keys;
    expect(keys.map((k) => [k.t, k.x])).toEqual([
      [0, 0],
      [2, 3],
    ]);
    expect(store().pending[CHAR]).toBeUndefined();
  });

  test("道具没有关键帧，直接改", () => {
    store().addObject("prop");
    const id = store().scene!.props[0].id;
    store().transformObject(id, { x: 5, w: 2 });
    expect(store().scene?.props[0]).toMatchObject({ x: 5, w: 2 });
  });

  test("机位高度不低于 0.1 米，俯仰不超过 ±85°", () => {
    store().transformObject(CAM, { y: -2, pitch: 120 });
    expect(store().scene?.cameras[0]).toMatchObject({ y: 0.1, pitch: 85 });
  });
});

describe("关键帧", () => {
  test("记录在播放头时刻，并选中这个关键帧", () => {
    store().setTime(1.5);
    store().recordKey(CAM);
    expect(store().scene?.cameras[0].keys.map((k) => k.t)).toEqual([1.5]);
    expect(store().selectedKey).toEqual({ id: CAM, t: 1.5 });
  });

  test("道具不能打关键帧", () => {
    store().addObject("prop");
    const id = store().scene!.props[0].id;
    expect(store().recordKey(id)).toBe(false);
  });

  test("删除选中的关键帧", () => {
    store().recordKey(CAM);
    store().deleteSelectedKey();
    expect(store().scene?.cameras[0].keys).toHaveLength(0);
    expect(store().selectedKey).toBeNull();
  });

  test("拖动关键帧改时间，算一步", () => {
    store().recordKey(CAM);
    store().moveKey(CAM, 0, 2);
    expect(store().scene?.cameras[0].keys[0].t).toBe(2);
    expect(store().selectedKey).toEqual({ id: CAM, t: 2 });
  });

  test("切换关键帧的插值方式，算一步", () => {
    store().recordKey(CAM);
    store().toggleKeyEase(CAM, 0);
    expect(store().scene?.cameras[0].keys[0].ease).toBe("linear");
    store().toggleKeyEase(CAM, 0);
    expect(store().scene?.cameras[0].keys[0].ease).toBe("smooth");
    expect(store().past).toHaveLength(3);
  });

  test("播放头夹在 0 到时长之间", () => {
    store().setTime(99);
    expect(store().t).toBe(5);
    store().setTime(-1);
    expect(store().t).toBe(0);
  });
});

describe("运镜预设与时长", () => {
  test("套用预设写入机位关键帧，算一步", () => {
    const result = store().applyPreset(CAM, "dollyIn");
    expect(result.warning).toBeUndefined();
    expect(store().scene?.cameras[0].keys).toHaveLength(2);
    expect(store().past).toHaveLength(1);
  });

  test("跟拍没有走位时返回提示", () => {
    expect(store().applyPreset(CAM, "follow").warning).toContain("走位");
  });

  test("改时长后播放头不越界", () => {
    store().setTime(5);
    store().setDuration(3);
    expect(store().t).toBe(3);
  });
});
