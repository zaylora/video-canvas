import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { Box3, type Group, type Object3D, Vector3 } from "three";

import type { PoseId } from "@/types";
import {
  FIGURE_STAND_HEIGHT,
  createFigure,
  figureModelStatus,
  loadFigureModel,
  measureFigure,
  resetFigureModel,
} from "@/utils/director/figure";
import { POSE_LABELS } from "@/utils/director/scene";

const MODEL = new URL("../../../../public/director/mannequin.glb", import.meta.url);

/** 读磁盘上的真实人台模型，代替浏览器里的请求 */
const readModel = async () => {
  const bytes = readFileSync(MODEL);
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
};

const POSES = Object.keys(POSE_LABELS) as PoseId[];

/** 人物在 pose 姿势下，某根骨骼在人物局部坐标里的位置 */
const bone = (group: Group, name: string) => {
  group.updateMatrixWorld(true);
  const found = group.getObjectByName(name) as Object3D;
  expect(found).toBeDefined();
  return found.getWorldPosition(new Vector3());
};

const figure = (pose: PoseId) => createFigure({ pose, color: "#d4d4d8" }).group;

afterEach(() => resetFigureModel());

describe("模型加载", () => {
  test("没加载时用简化形体（胶囊 + 头）顶着，不会没有人物", () => {
    expect(figureModelStatus()).toBe("idle");
    const { group } = createFigure({ pose: "stand", color: "#fca5a5" });
    expect(group.children.length).toBeGreaterThan(0);
    expect(group.getObjectByName("hand_l")).toBeUndefined();
  });

  test("加载成功后状态变 ready，人物换成带骨骼的人台", async () => {
    expect(await loadFigureModel(readModel)).toBe(true);
    expect(figureModelStatus()).toBe("ready");
    expect(figure("stand").getObjectByName("hand_l")).toBeDefined();
  });

  test("同时多次加载只读一次文件", async () => {
    let reads = 0;
    const counting = () => {
      reads++;
      return readModel();
    };
    await Promise.all([loadFigureModel(counting), loadFigureModel(counting)]);
    await loadFigureModel(counting);
    expect(reads).toBe(1);
  });

  test("加载失败：状态 failed，仍能画出简化形体，之后可以重试", async () => {
    const broken = () => Promise.reject(new Error("网络断了"));
    expect(await loadFigureModel(broken)).toBe(false);
    expect(figureModelStatus()).toBe("failed");
    expect(createFigure({ pose: "stand", color: "#fff" }).group.children.length).toBeGreaterThan(0);
    expect(await loadFigureModel(readModel)).toBe(true);
    expect(figureModelStatus()).toBe("ready");
  });

  test("文件不是合法的模型也算失败", async () => {
    const garbage = async () => new TextEncoder().encode("not a glb").buffer as ArrayBuffer;
    expect(await loadFigureModel(garbage)).toBe(false);
    expect(figureModelStatus()).toBe("failed");
  });
});

describe("人台的朝向、身高与落地", () => {
  beforeEach(async () => {
    await loadFigureModel(readModel);
  });

  test("站立时身高约 1.78 米，脚踩在地面上", () => {
    const box = measureFigure(figure("stand"));
    expect(box.min.y).toBeCloseTo(0, 4);
    expect(box.max.y).toBeCloseTo(FIGURE_STAND_HEIGHT, 1);
  });

  test("脸朝本地 -z：脚尖在脚踝前面（z 更小）", () => {
    const stand = figure("stand");
    expect(bone(stand, "ball_l").z).toBeLessThan(bone(stand, "foot_l").z - 0.05);
  });

  test("人物自己的左手在 -x 一侧（面朝 -z 时右手在 +x）", () => {
    const stand = figure("stand");
    expect(bone(stand, "hand_l").x).toBeLessThan(-0.1);
    expect(bone(stand, "hand_r").x).toBeGreaterThan(0.1);
  });

  test("12 个姿势都能摆出来，脚（或身体）刚好落在地面上，没有穿地", () => {
    for (const pose of POSES) {
      const box = measureFigure(figure(pose));
      expect(box.min.y).toBeCloseTo(0, 4);
      expect(box.max.y).toBeGreaterThan(0.2);
    }
  });

  test("换颜色只改自己的材质，不影响别的人物", () => {
    const a = createFigure({ pose: "stand", color: "#ff0000" });
    const b = createFigure({ pose: "stand", color: "#0000ff" });
    expect(a.material).not.toBe(b.material);
    expect(a.material.color.getHexString()).toBe("ff0000");
    expect(b.material.color.getHexString()).toBe("0000ff");
  });
});

describe("12 个姿势的特征", () => {
  beforeEach(async () => {
    await loadFigureModel(readModel);
  });

  test("站立：双臂自然下垂，手垂在肩的正下方、靠身体两侧", () => {
    const stand = figure("stand");
    for (const side of ["l", "r"]) {
      const shoulder = bone(stand, `upperarm_${side}`);
      const hand = bone(stand, `hand_${side}`);
      expect(hand.y).toBeLessThan(shoulder.y - 0.35);
      expect(Math.abs(hand.x - shoulder.x)).toBeLessThan(0.2);
    }
  });

  test("坐：大腿水平、头的高度明显降低，脚仍在地面", () => {
    const sit = figure("sit");
    const knee = bone(sit, "calf_l");
    const hip = bone(sit, "thigh_l");
    expect(Math.abs(knee.y - hip.y)).toBeLessThan(0.08);
    expect(knee.z).toBeLessThan(hip.z - 0.3);
    expect(bone(sit, "Head").y).toBeLessThan(1.3);
    expect(bone(sit, "Head").y).toBeGreaterThan(0.9);
  });

  test("蹲：比坐更矮，膝盖在前", () => {
    const crouch = figure("crouch");
    expect(bone(crouch, "Head").y).toBeLessThan(1.15);
    expect(bone(crouch, "calf_l").z).toBeLessThan(bone(crouch, "thigh_l").z - 0.2);
  });

  test("行走：两条腿一前一后", () => {
    const walk = figure("walk");
    const gap = Math.abs(bone(walk, "foot_l").z - bone(walk, "foot_r").z);
    expect(gap).toBeGreaterThan(0.25);
  });

  test("奔跑：步幅比行走更大，上身前倾", () => {
    const run = figure("run");
    const walk = figure("walk");
    const gap = (g: Group) => Math.abs(bone(g, "foot_l").z - bone(g, "foot_r").z);
    expect(gap(run)).toBeGreaterThan(gap(walk));
    expect(bone(run, "Head").z).toBeLessThan(bone(run, "pelvis").z - 0.1);
  });

  test("单手指向：右手伸到身体前方、大约和肩同高", () => {
    const point = figure("point");
    const shoulder = bone(point, "upperarm_r");
    const hand = bone(point, "hand_r");
    expect(hand.z).toBeLessThan(shoulder.z - 0.45);
    expect(Math.abs(hand.y - shoulder.y)).toBeLessThan(0.25);
  });

  test("双手抱胸：两只手都在胸前，靠近身体中线", () => {
    const crossed = figure("armsCrossed");
    for (const side of ["l", "r"]) {
      const hand = bone(crossed, `hand_${side}`);
      expect(Math.abs(hand.x)).toBeLessThan(0.3);
      expect(hand.y).toBeGreaterThan(1.0);
      expect(hand.y).toBeLessThan(1.45);
      expect(hand.z).toBeLessThan(bone(crossed, "spine_03").z - 0.05);
    }
  });

  test("挥手：右手举过头顶，向外侧伸出", () => {
    const wave = figure("wave");
    expect(bone(wave, "hand_r").y).toBeGreaterThan(bone(wave, "Head").y);
    expect(bone(wave, "hand_r").x).toBeGreaterThan(0.2);
  });

  test("对话：右手抬到胸前打手势", () => {
    const talk = figure("talk");
    expect(bone(talk, "hand_r").y).toBeGreaterThan(1.0);
    expect(bone(talk, "hand_r").z).toBeLessThan(bone(talk, "spine_03").z - 0.2);
  });

  test("举手：右手笔直举过头顶", () => {
    const raise = figure("raiseHand");
    const hand = bone(raise, "hand_r");
    expect(hand.y).toBeGreaterThan(bone(raise, "Head").y + 0.2);
    expect(Math.abs(hand.x - bone(raise, "upperarm_r").x)).toBeLessThan(0.2);
  });

  test("回头：头转向一侧，身体不动", () => {
    const stand = figure("stand");
    const look = figure("lookBack");
    const facing = (g: Group) => {
      g.updateMatrixWorld(true);
      const head = g.getObjectByName("Head")!;
      const nose = new Vector3(0, 0, 1).transformDirection(head.matrixWorld);
      return Math.atan2(nose.x, nose.z);
    };
    expect(Math.abs(facing(look) - facing(stand))).toBeGreaterThan(0.8);
    expect(bone(look, "pelvis").x).toBeCloseTo(bone(stand, "pelvis").x, 3);
  });

  test("倒地：仰面躺平，身体沿 z 方向伸展、很矮", () => {
    const box = measureFigure(figure("lying"));
    expect(box.max.y).toBeLessThan(0.5);
    expect(box.max.z - box.min.z).toBeGreaterThan(1.4);
  });
});

describe("几何体尺寸", () => {
  test("measureFigure 返回人物局部坐标里的包围盒", async () => {
    await loadFigureModel(readModel);
    const box = measureFigure(figure("stand"));
    expect(box).toBeInstanceOf(Box3);
    expect(box.getSize(new Vector3()).y).toBeCloseTo(FIGURE_STAND_HEIGHT, 1);
  });
});
