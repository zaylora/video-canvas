import { describe, expect, test } from "bun:test";

import {
  MAX_FOV,
  MIN_FOV,
  grabDepth,
  grabLook,
  moveInput,
  normalizeWheel,
  panPosition,
  pixelRay,
  rotateByLook,
  tiltByWheel,
  walk,
  zoomByWheel,
} from "@/utils/director/free-look";

const DEG = Math.PI / 180;
const dot = (a: number[], b: number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const expectVec = (actual: number[], expected: number[], digits = 9) => {
  for (let i = 0; i < 3; i++) expect(actual[i]).toBeCloseTo(expected[i], digits);
};

describe("屏幕像素对应的视线方向", () => {
  test("画面正中是正前方（-z），是单位向量", () => {
    expectVec(pixelRay(0, 0, 50, 16 / 9), [0, 0, -1]);
  });

  test("右边缘：水平方向的半角 = atan(tan(竖直半角) × 宽高比)", () => {
    const ray = pixelRay(1, 0, 50, 2);
    expect(Math.atan2(ray[0], -ray[2]) / DEG).toBeCloseTo(
      Math.atan(Math.tan(25 * DEG) * 2) / DEG,
      8,
    );
    expect(Math.hypot(...ray)).toBeCloseTo(1, 12);
  });

  test("上边缘：竖直半角 = 视场角的一半", () => {
    const ray = pixelRay(0, 1, 50, 2);
    expect(Math.atan2(ray[1], -ray[2]) / DEG).toBeCloseTo(25, 8);
  });
});

describe("按偏航、俯仰转向", () => {
  test("偏航 0、俯仰 0 面朝 -z；偏航 90 面朝 +x；俯仰为正抬头", () => {
    expectVec(rotateByLook(0, 0, [0, 0, -1]), [0, 0, -1]);
    expectVec(rotateByLook(90, 0, [0, 0, -1]), [1, 0, 0]);
    expectVec(rotateByLook(0, 30, [0, 0, -1]), [0, Math.sin(30 * DEG), -Math.cos(30 * DEG)]);
  });

  test("和舞台里机位的约定一致：偏航 -90 面朝 -x，右手边（画面右侧）随之转", () => {
    expectVec(rotateByLook(-90, 0, [0, 0, -1]), [-1, 0, 0]);
    // 面朝 -z 时画面右侧是 +x
    expectVec(rotateByLook(0, 0, [1, 0, 0]), [1, 0, 0]);
  });
});

describe("抓手：按下时鼠标下的那个点，拖到哪画面就跟到哪", () => {
  const VFOV = 50;
  const ASPECT = 16 / 9;

  test("没动鼠标，朝向不变", () => {
    const start = { yaw: 33, pitch: -12 };
    const next = grabLook(start, { x: 0.3, y: -0.2 }, { x: 0.3, y: -0.2 }, VFOV, ASPECT);
    expect(next.yaw).toBeCloseTo(33, 8);
    expect(next.pitch).toBeCloseTo(-12, 8);
  });

  test("从画面正中向右拖：机位向左转，转过的角度正好让右边那个点来到正前方", () => {
    const next = grabLook({ yaw: 0, pitch: 0 }, { x: 0, y: 0 }, { x: 0.5, y: 0 }, VFOV, ASPECT);
    // 鼠标到了 ndc x = 0.5 处，原来正前方的点现在应该在那儿
    const expected = -Math.atan(Math.tan(25 * DEG) * ASPECT * 0.5) / DEG;
    expect(next.yaw).toBeCloseTo(expected, 8);
    expect(next.pitch).toBeCloseTo(0, 8);
  });

  test("向下拖：机位向上抬", () => {
    const next = grabLook({ yaw: 0, pitch: 0 }, { x: 0, y: 0 }, { x: 0, y: -0.5 }, VFOV, ASPECT);
    expect(next.pitch).toBeGreaterThan(0);
    expect(next.yaw).toBeCloseTo(0, 8);
  });

  test("精确：不管从哪个像素抓、拖到哪、视场角多大、机位朝哪，按下时那点的方向都和鼠标下的方向重合", () => {
    const starts = [
      { yaw: 0, pitch: 0 },
      { yaw: 120, pitch: 40 },
      { yaw: -75, pitch: -50 },
      { yaw: 179, pitch: 70 },
    ];
    const pixels = [
      [0.9, 0.8],
      [-0.9, 0.7],
      [0.1, -0.95],
      [-0.6, -0.3],
      [0, 0],
    ];
    for (const start of starts) {
      for (const vfov of [20, 50, 100]) {
        for (const aspect of [9 / 16, 1, 16 / 9, 21 / 9]) {
          for (const from of pixels) {
            for (const to of pixels) {
              const a = { x: from[0], y: from[1] };
              const b = { x: to[0], y: to[1] };
              const pinned = rotateByLook(start.yaw, start.pitch, pixelRay(a.x, a.y, vfov, aspect));
              const next = grabLook(start, a, b, vfov, aspect);
              // 抓到的那个方向，现在应该正好在鼠标 b 下面
              const now = rotateByLook(next.yaw, next.pitch, pixelRay(b.x, b.y, vfov, aspect));
              // 够不到的情况除外：抓住的方向比鼠标下的视线更高 / 更低，转不过去；能转到的时候必须精确
              const reach = Math.hypot(...pixelRay(b.x, b.y, vfov, aspect).slice(1));
              if (Math.abs(pinned[1]) < reach - 1e-9 && Math.abs(next.pitch) < 84.999) {
                expectVec(now, pinned, 7);
              }
            }
          }
        }
      }
    }
  });

  test("拖得再远俯仰也不会越过 ±85°，偏航收在 [-180, 180) 里", () => {
    const next = grabLook(
      { yaw: 170, pitch: 80 },
      { x: 0, y: 0 },
      { x: -0.9, y: -0.9 },
      50,
      16 / 9,
    );
    expect(Math.abs(next.pitch)).toBeLessThanOrEqual(85);
    expect(next.yaw).toBeGreaterThanOrEqual(-180);
    expect(next.yaw).toBeLessThan(180);
  });
});

describe("滚轮", () => {
  test("统一成像素：按行滚动的滚轮换算成像素", () => {
    expect(normalizeWheel(100, 0)).toBe(100);
    expect(normalizeWheel(3, 1)).toBeGreaterThan(30);
    expect(normalizeWheel(1, 2)).toBeGreaterThan(300);
  });

  test("上摇（滚轮向上，deltaY 为负）：视角向上抬；下摇：向下", () => {
    expect(tiltByWheel(0, -100)).toBeGreaterThan(0);
    expect(tiltByWheel(0, 100)).toBeLessThan(0);
  });

  test("一格滚轮转几度是固定的，和当前俯仰无关", () => {
    expect(tiltByWheel(10, -100) - 10).toBeCloseTo(tiltByWheel(-20, -100) + 20, 10);
  });

  test("俯仰夹在 ±85° 以内", () => {
    expect(tiltByWheel(84, -1000)).toBe(85);
    expect(tiltByWheel(-84, 1000)).toBe(-85);
  });

  test("变焦：滚轮向上拉近（视场角变小），向下拉远；夹在 10°~120°", () => {
    expect(zoomByWheel(50, -100)).toBeLessThan(50);
    expect(zoomByWheel(50, 100)).toBeGreaterThan(50);
    expect(zoomByWheel(11, -10000)).toBe(MIN_FOV);
    expect(zoomByWheel(119, 10000)).toBe(MAX_FOV);
  });

  test("变焦按比例：拉近一格再拉远一格回到原处", () => {
    expect(zoomByWheel(zoomByWheel(50, -100), 100)).toBeCloseTo(50, 8);
  });
});

describe("键盘平移", () => {
  test("WASD 和方向键等价：W / ↑ 前，S / ↓ 后，D / → 右，A / ← 左", () => {
    expect(moveInput(new Set(["w"]))).toEqual({ forward: 1, right: 0 });
    expect(moveInput(new Set(["arrowup"]))).toEqual({ forward: 1, right: 0 });
    expect(moveInput(new Set(["s"]))).toEqual({ forward: -1, right: 0 });
    expect(moveInput(new Set(["arrowdown"]))).toEqual({ forward: -1, right: 0 });
    expect(moveInput(new Set(["d"]))).toEqual({ forward: 0, right: 1 });
    expect(moveInput(new Set(["arrowright"]))).toEqual({ forward: 0, right: 1 });
    expect(moveInput(new Set(["a"]))).toEqual({ forward: 0, right: -1 });
    expect(moveInput(new Set(["arrowleft"]))).toEqual({ forward: 0, right: -1 });
  });

  test("相反的键同时按住互相抵消，别的键不算", () => {
    expect(moveInput(new Set(["w", "s"]))).toEqual({ forward: 0, right: 0 });
    expect(moveInput(new Set(["w", "arrowdown"]))).toEqual({ forward: 0, right: 0 });
    expect(moveInput(new Set(["q", "x", "1"]))).toEqual({ forward: 0, right: 0 });
  });

  test("往前走沿朝向的水平方向，高度不变，和俯仰无关", () => {
    const at0 = walk({ x: 0, y: 1.6, z: 0 }, 0, { forward: 1, right: 0 }, 2);
    expectVec([at0.x, at0.y, at0.z], [0, 1.6, -2]);
    const at90 = walk({ x: 0, y: 1.6, z: 0 }, 90, { forward: 1, right: 0 }, 2);
    expectVec([at90.x, at90.y, at90.z], [2, 1.6, 0]);
  });

  test("往右走：面朝 -z 时是 +x，面朝 +x 时是 +z", () => {
    const a = walk({ x: 0, y: 1, z: 0 }, 0, { forward: 0, right: 1 }, 1);
    expectVec([a.x, a.y, a.z], [1, 1, 0]);
    const b = walk({ x: 0, y: 1, z: 0 }, 90, { forward: 0, right: 1 }, 1);
    expectVec([b.x, b.y, b.z], [0, 1, 1]);
  });

  test("斜着走（前 + 右）速度不变，不会比直走更快", () => {
    const moved = walk({ x: 0, y: 1, z: 0 }, 30, { forward: 1, right: 1 }, 3);
    expect(Math.hypot(moved.x, moved.z)).toBeCloseTo(3, 10);
  });

  test("没按键不动", () => {
    expect(walk({ x: 1, y: 2, z: 3 }, 45, { forward: 0, right: 0 }, 5)).toEqual({
      x: 1,
      y: 2,
      z: 3,
    });
  });
});

describe("按住拖动平移（导演视角的右键）", () => {
  const VFOV = 50;
  const ASPECT = 16 / 9;
  const pose = { x: 1, y: 3, z: 6, yaw: 25, pitch: -20 };

  test("按下时鼠标下的那个点，拖到哪画面就跟到哪", () => {
    const from = { x: 0.2, y: -0.1 };
    const to = { x: -0.5, y: 0.4 };
    const dir = rotateByLook(pose.yaw, pose.pitch, pixelRay(from.x, from.y, VFOV, ASPECT));
    const depth = 7;
    const pinned = [pose.x + dir[0] * depth, pose.y + dir[1] * depth, pose.z + dir[2] * depth];
    const next = panPosition(pose, pinned, to, VFOV, ASPECT);
    // 新位置看向鼠标 b 的方向，应该正好穿过被抓住的点
    const ray = rotateByLook(pose.yaw, pose.pitch, pixelRay(to.x, to.y, VFOV, ASPECT));
    const toPoint = [pinned[0] - next.x, pinned[1] - next.y, pinned[2] - next.z];
    const len = Math.hypot(...toPoint);
    expectVec(
      toPoint.map((v) => v / len),
      ray,
      9,
    );
  });

  test("平移不改变朝向，被抓的点到相机的前向深度不变", () => {
    const forward = rotateByLook(pose.yaw, pose.pitch, [0, 0, -1]);
    const dir = rotateByLook(pose.yaw, pose.pitch, pixelRay(0.3, 0.2, VFOV, ASPECT));
    const pinned = [pose.x + dir[0] * 5, pose.y + dir[1] * 5, pose.z + dir[2] * 5];
    const next = panPosition(pose, pinned, { x: -0.4, y: -0.3 }, VFOV, ASPECT);
    const before = dot([pinned[0] - pose.x, pinned[1] - pose.y, pinned[2] - pose.z], forward);
    const after = dot([pinned[0] - next.x, pinned[1] - next.y, pinned[2] - next.z], forward);
    expect(after).toBeCloseTo(before, 9);
  });

  test("没动鼠标，位置不变", () => {
    const dir = rotateByLook(pose.yaw, pose.pitch, pixelRay(0.1, 0.1, VFOV, ASPECT));
    const pinned = [pose.x + dir[0] * 4, pose.y + dir[1] * 4, pose.z + dir[2] * 4];
    const next = panPosition(pose, pinned, { x: 0.1, y: 0.1 }, VFOV, ASPECT);
    expectVec([next.x, next.y, next.z], [pose.x, pose.y, pose.z]);
  });

  test("抓取深度：视线落在地面上就取到地面的距离，往天上看就用默认深度", () => {
    expect(grabDepth({ x: 0, y: 4, z: 0 }, [0, -1, 0])).toBeCloseTo(4, 10);
    expect(grabDepth({ x: 0, y: 1, z: 0 }, [0, 1, 0])).toBe(8);
    expect(grabDepth({ x: 0, y: 1, z: 0 }, [1, 0, 0])).toBe(8);
  });

  test("地面太近或太远时夹在 0.5 ~ 60 米", () => {
    expect(grabDepth({ x: 0, y: 0.01, z: 0 }, [0, -1, 0])).toBe(0.5);
    expect(grabDepth({ x: 0, y: 500, z: 0 }, [0, -1, 0])).toBe(60);
  });
});
