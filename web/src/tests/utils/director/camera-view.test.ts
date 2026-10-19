import { describe, expect, test } from "bun:test";

import { MAX_PITCH, activeCameraOf, lookBy, viewFov, wrapYaw } from "@/utils/director/camera-view";
import { addCamera, createDefaultScene } from "@/utils/director/scene";

const RAD = Math.PI / 180;

describe("机位视角的视场角适配", () => {
  test("视口比机位画幅更宽（或一样宽）：竖直视场角照用，画幅左右留白", () => {
    expect(viewFov(50, 16 / 9, 16 / 9)).toBe(50);
    expect(viewFov(50, 16 / 9, 3)).toBe(50);
  });

  test("视口比画幅更窄：放大竖直视场角，保证画幅的左右边刚好顶到视口两边", () => {
    const fov = viewFov(50, 16 / 9, 1);
    // 视口宽度对应的水平半宽，要等于机位画幅的水平半宽
    expect(Math.tan((fov / 2) * RAD) * 1).toBeCloseTo(Math.tan(25 * RAD) * (16 / 9), 8);
    expect(fov).toBeGreaterThan(50);
  });

  test("竖画幅机位放进横向视口：竖直视场角照用", () => {
    expect(viewFov(40, 9 / 16, 16 / 9)).toBe(40);
  });

  test("视口还没量出来（宽高比无效）时原样返回", () => {
    expect(viewFov(50, 16 / 9, 0)).toBe(50);
    expect(viewFov(50, 16 / 9, Number.NaN)).toBe(50);
  });
});

describe("偏航角归一", () => {
  test("收回 [-180, 180) 里，方向不变", () => {
    expect(wrapYaw(190)).toBeCloseTo(-170, 10);
    expect(wrapYaw(-190)).toBeCloseTo(170, 10);
    expect(wrapYaw(720 + 30)).toBeCloseTo(30, 10);
    expect(wrapYaw(45)).toBe(45);
    expect(wrapYaw(0)).toBe(0);
  });
});

describe("鼠标拖动调整机位朝向", () => {
  const pose = { yaw: 0, pitch: 0 };

  test("向右拖：画面跟着手向右走，机位向左转（偏航减小）", () => {
    const next = lookBy(pose, 100, 0, 50, 500);
    expect(next.yaw).toBeCloseTo(-10, 10);
    expect(next.pitch).toBe(0);
  });

  test("向下拖：画面跟着手向下走，机位向上抬（俯仰增大）", () => {
    const next = lookBy(pose, 0, 100, 50, 500);
    expect(next.pitch).toBeCloseTo(10, 10);
    expect(next.yaw).toBe(0);
  });

  test("每像素的角度 = 视场角 / 视口高度，所以画面里的东西跟手 1:1", () => {
    const next = lookBy(pose, 0, 250, 60, 500);
    expect(next.pitch).toBeCloseTo(30, 10);
  });

  test("视场角越小（长焦）拖同样的像素转得越少", () => {
    const wide = lookBy(pose, 50, 0, 80, 500);
    const tele = lookBy(pose, 50, 0, 20, 500);
    expect(Math.abs(tele.yaw)).toBeLessThan(Math.abs(wide.yaw));
  });

  test("俯仰夹在 ±MAX_PITCH 以内，抬过头就停住", () => {
    expect(lookBy({ yaw: 0, pitch: 80 }, 0, 500, 50, 500).pitch).toBe(MAX_PITCH);
    expect(lookBy({ yaw: 0, pitch: -80 }, 0, -500, 50, 500).pitch).toBe(-MAX_PITCH);
  });

  test("偏航转过 180° 会绕回来，不会越积越大", () => {
    const next = lookBy({ yaw: 175, pitch: 0 }, -100, 0, 50, 500);
    expect(next.yaw).toBeCloseTo(-175, 10);
  });

  test("视口高度无效时不动", () => {
    expect(lookBy({ yaw: 10, pitch: 5 }, 100, 100, 50, 0)).toEqual({ yaw: 10, pitch: 5 });
  });
});

describe("当前是不是机位视角", () => {
  const scene = () => {
    let n = 0;
    return createDefaultScene(() => `id${++n}`);
  };

  test("机位视角看的是当前机位", () => {
    const s = scene();
    expect(activeCameraOf(s, "camera")).toBe(s.activeCameraId);
    expect(activeCameraOf(s, "camera")).toBe("id2");
  });

  test("导演视角没有机位 id", () => {
    expect(activeCameraOf(scene(), "director")).toBeNull();
  });

  test("没有场景、没有机位、当前机位不存在时，都退回导演视角", () => {
    expect(activeCameraOf(null, "camera")).toBeNull();
    expect(activeCameraOf({ ...scene(), cameras: [] }, "camera")).toBeNull();
    expect(activeCameraOf({ ...scene(), activeCameraId: "" }, "camera")).toBeNull();
  });

  test("切换当前机位后跟着换", () => {
    const added = addCamera(scene(), () => "c3").scene;
    expect(activeCameraOf({ ...added, activeCameraId: "c3" }, "camera")).toBe("c3");
  });
});
