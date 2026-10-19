import { describe, expect, test } from "bun:test";

import type { DirectorScene } from "@/types";
import { CAMERA_PRESETS, applyPreset } from "@/utils/director/preset";
import { sampleCamera } from "@/utils/director/sample";
import { createDefaultScene } from "@/utils/director/scene";

/** 默认场景：人物在原点，机位在 (0, 1.6, 4) 看向人物 */
const base = (): DirectorScene => {
  let n = 0;
  return createDefaultScene(() => `id${++n}`);
};

const run = (scene: DirectorScene, preset: string, strength?: number) => {
  const camera = scene.cameras[0];
  return applyPreset(scene, camera.id, preset as never, strength);
};

const last = (scene: DirectorScene) => scene.cameras[0].keys.at(-1)!;

describe("预设清单", () => {
  test("包含设计稿里的 13 个预设，每个都有中文名", () => {
    expect(CAMERA_PRESETS.map((p) => p.label)).toEqual([
      "固定",
      "推近",
      "拉远",
      "左摇",
      "右摇",
      "上摇",
      "下摇",
      "左移",
      "右移",
      "升",
      "降",
      "环绕",
      "跟拍",
    ]);
  });
});

describe("两帧类预设", () => {
  test("推近：沿视线前进 目标距离 × 强度", () => {
    const { scene } = run(base(), "dollyIn", 50);
    const keys = scene.cameras[0].keys;
    expect(keys.map((k) => k.t)).toEqual([0, 5]);
    expect(keys[0].ease).toBe("smooth");
    // 目标是人物胸口 (0, 1.3, 0)，机位正看着它，前进一半距离后落在 z=2
    expect(last(scene).z).toBeCloseTo(2, 6);
    expect(last(scene).x).toBeCloseTo(0, 6);
  });

  test("拉远：反方向", () => {
    expect(last(run(base(), "dollyOut", 50).scene).z).toBeGreaterThan(5.9);
  });

  test("右摇：原地偏航增加", () => {
    const end = last(run(base(), "panRight", 30).scene);
    expect(end.yaw).toBeCloseTo(30, 6);
    expect(end.x).toBe(0);
  });

  test("上摇：俯仰增加，不超过 85°", () => {
    const start = base().cameras[0].pitch;
    expect(last(run(base(), "tiltUp", 15).scene).pitch).toBeCloseTo(start + 15, 6);
    expect(last(run(base(), "tiltUp", 200).scene).pitch).toBeLessThanOrEqual(85);
  });

  test("右移：沿机位右方向平移，朝向不变", () => {
    const end = last(run(base(), "truckRight", 2).scene);
    expect(end.x).toBeCloseTo(2, 6);
    expect(end.z).toBeCloseTo(4, 6);
    expect(end.yaw).toBe(0);
  });

  test("升：高度增加并低头看回目标", () => {
    const end = last(run(base(), "craneUp", 1.5).scene);
    expect(end.y).toBeCloseTo(3.1, 6);
    expect(end.pitch).toBeLessThan(-15);
  });

  test("降：不低于 0.2 米", () => {
    expect(last(run(base(), "craneDown", 5).scene).y).toBe(0.2);
  });

  test("没传强度用默认值", () => {
    const preset = CAMERA_PRESETS.find((p) => p.id === "panLeft")!;
    expect(last(run(base(), "panLeft").scene).yaw).toBeCloseTo(-preset.defaultStrength, 6);
  });
});

describe("烘焙类预设", () => {
  test("环绕 90°：每 0.5 秒一帧、线性，始终看向目标、距离不变", () => {
    const { scene } = run(base(), "orbit", 90);
    const keys = scene.cameras[0].keys;
    expect(keys).toHaveLength(11);
    expect(keys.every((k) => k.ease === "linear")).toBe(true);
    const end = keys.at(-1)!;
    expect(Math.hypot(end.x, end.z)).toBeCloseTo(4, 6);
    expect(end.x).toBeCloseTo(-4, 6);
    expect(end.z).toBeCloseTo(0, 6);
    // 站在 -x 一侧看原点，朝向 +x
    expect(end.yaw).toBeCloseTo(90, 6);
  });

  test("跟拍：保持与人物的相对位置", () => {
    const scene = base();
    const character = scene.characters[0];
    character.keys = [
      { t: 0, x: -2, z: 0, yaw: 90, ease: "smooth" },
      { t: 5, x: 2, z: 0, yaw: 90, ease: "smooth" },
    ];
    const { scene: next, warning } = run(scene, "follow");
    expect(warning).toBeUndefined();
    const cam = next.cameras[0];
    expect(cam.keys).toHaveLength(11);
    // 起点机位 x=0、人物 x=-2，相距 2 米；人物走到 x=2，机位跟到 x=4
    expect(sampleCamera(cam, 0).x).toBeCloseTo(0, 6);
    expect(sampleCamera(cam, 5).x).toBeCloseTo(4, 6);
  });

  test("跟拍的人物没有走位：效果等同固定，并给出提示", () => {
    const { scene, warning } = run(base(), "follow");
    expect(warning).toContain("走位");
    const keys = scene.cameras[0].keys;
    expect(keys.every((k) => k.x === keys[0].x && k.z === keys[0].z)).toBe(true);
  });

  test("跟拍但没有可跟随的人物：只留一帧并提示", () => {
    const scene = base();
    scene.characters = [];
    scene.cameras[0].lookAt = undefined;
    const result = run(scene, "follow");
    expect(result.warning).toContain("看向");
    expect(result.scene.cameras[0].keys).toHaveLength(1);
  });
});

describe("目标与起点", () => {
  test("没设看向时，取视线前方最近的人物", () => {
    const scene = base();
    scene.cameras[0].lookAt = undefined;
    expect(last(run(scene, "dollyIn", 50).scene).z).toBeCloseTo(2, 6);
  });

  test("前方没有人物时，目标是前方 4 米的点", () => {
    const scene = base();
    scene.characters = [];
    scene.cameras[0].lookAt = undefined;
    // 视线略向下，水平分量约 1.99 米
    expect(last(run(scene, "dollyIn", 50).scene).z).toBeCloseTo(2, 1);
  });

  test("起点取机位在 0 秒的状态", () => {
    const scene = base();
    const cam = scene.cameras[0];
    cam.keys = [{ ...cam, x: 1, t: 0, ease: "smooth" }];
    expect(last(run(scene, "truckRight", 2).scene).x).toBeCloseTo(3, 6);
  });

  test("固定：清成一帧", () => {
    const scene = run(base(), "orbit", 90).scene;
    const fixed = applyPreset(scene, scene.cameras[0].id, "fixed").scene;
    expect(fixed.cameras[0].keys).toHaveLength(1);
    expect(fixed.cameras[0].keys[0].t).toBe(0);
  });

  test("不改动传入的场景", () => {
    const scene = base();
    run(scene, "dollyIn");
    expect(scene.cameras[0].keys).toHaveLength(0);
  });

  test("机位不存在时原样返回", () => {
    const scene = base();
    expect(applyPreset(scene, "nope", "dollyIn").scene).toBe(scene);
  });
});
