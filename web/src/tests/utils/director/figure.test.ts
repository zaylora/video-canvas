import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { Box3, type Group, type Object3D, Vector3 } from "three";

import type { FigureModel, PoseId } from "@/types";
import {
  FIGURE_HEIGHT,
  createFigure,
  figureModelStatus,
  loadFigureModel,
  loadFigureModels,
  measureFigure,
  readWithProgress,
  resetFigureModel,
} from "@/utils/director/figure";
import { POSE_LABELS } from "@/utils/director/scene";

const MODELS: FigureModel[] = ["male", "female"];

/** 读磁盘上的真实人台模型，代替浏览器里的请求 */
const readModel = async (model: FigureModel) => {
  const bytes = readFileSync(new URL(`../../../../public/director/${model}.glb`, import.meta.url));
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
};

const POSES = Object.keys(POSE_LABELS) as PoseId[];

/** 人物在某个姿势下，某根骨骼在人物局部坐标里的位置 */
const bone = (group: Group, name: string) => {
  group.updateMatrixWorld(true);
  const found = group.getObjectByName(name) as Object3D;
  expect(found).toBeDefined();
  return found.getWorldPosition(new Vector3());
};

afterEach(() => resetFigureModel());

describe("模型加载", () => {
  test("没加载时用简化形体（胶囊 + 头）顶着，不会没有人物", () => {
    expect(figureModelStatus("male")).toBe("idle");
    const { group } = createFigure({ pose: "stand", color: "#fca5a5" });
    expect(group.children.length).toBeGreaterThan(0);
    expect(group.getObjectByName("hand_l")).toBeUndefined();
  });

  test("男、女模型各自加载，互不影响", async () => {
    expect(await loadFigureModel(readModel, "female")).toBe(true);
    expect(figureModelStatus("female")).toBe("ready");
    expect(figureModelStatus("male")).toBe("idle");
    expect(
      createFigure({ pose: "stand", color: "#fff", model: "female" }).group.getObjectByName(
        "hand_l",
      ),
    ).toBeDefined();
    // 男性还没加载：画的是简化形体
    expect(
      createFigure({ pose: "stand", color: "#fff", model: "male" }).group.getObjectByName("hand_l"),
    ).toBeUndefined();
  });

  test("同一个模型同时多次加载只读一次文件", async () => {
    let reads = 0;
    const counting = (model: FigureModel) => {
      reads++;
      return readModel(model);
    };
    await Promise.all([loadFigureModel(counting), loadFigureModel(counting)]);
    await loadFigureModel(counting);
    expect(reads).toBe(1);
  });

  test("按场景里用到的模型加载，没用到的不读", async () => {
    const asked: FigureModel[] = [];
    const record = (model: FigureModel) => {
      asked.push(model);
      return readModel(model);
    };
    expect(await loadFigureModels(["male", "male"], record)).toBe(true);
    expect(asked).toEqual(["male"]);
    expect(figureModelStatus("female")).toBe("idle");
  });

  test("多个模型里有一个失败，整体算失败，成功的那个照常能用", async () => {
    const flaky = (model: FigureModel) =>
      model === "female" ? Promise.reject(new Error("断网")) : readModel(model);
    expect(await loadFigureModels(["male", "female"], flaky)).toBe(false);
    expect(figureModelStatus("male")).toBe("ready");
    expect(figureModelStatus("female")).toBe("failed");
  });

  test("加载失败：状态 failed，仍能画出简化形体，之后可以重试", async () => {
    const broken = () => Promise.reject(new Error("网络断了"));
    expect(await loadFigureModel(broken)).toBe(false);
    expect(figureModelStatus("male")).toBe("failed");
    expect(createFigure({ pose: "stand", color: "#fff" }).group.children.length).toBeGreaterThan(0);
    expect(await loadFigureModel(readModel)).toBe(true);
    expect(figureModelStatus("male")).toBe("ready");
  });

  test("文件不是合法的模型也算失败", async () => {
    const garbage = async () => new TextEncoder().encode("not a glb").buffer as ArrayBuffer;
    expect(await loadFigureModel(garbage)).toBe(false);
    expect(figureModelStatus("male")).toBe("failed");
  });
});

describe("模型加载进度", () => {
  /** 一边下载一边报告已下载比例的假读取函数 */
  const reporting = async (model: FigureModel, report?: (fraction: number) => void) => {
    report?.(0.25);
    report?.(0.5);
    report?.(1);
    return readModel(model);
  };

  test("下载中按比例上报，解析完成时是 1，且不会往回走", async () => {
    const seen: number[] = [];
    await loadFigureModel(reporting, "male", (f) => seen.push(f));
    expect(seen.length).toBeGreaterThan(1);
    expect(seen).toEqual([...seen].sort((a, b) => a - b));
    expect(seen.every((f) => f >= 0 && f <= 1)).toBe(true);
    // 下载完还要解析，解析完才算 1
    expect(seen.at(-2)).toBeLessThan(1);
    expect(seen.at(-1)).toBe(1);
  });

  test("已经加载好的模型立刻报 1", async () => {
    await loadFigureModel(readModel);
    const seen: number[] = [];
    await loadFigureModel(readModel, "male", (f) => seen.push(f));
    expect(seen).toEqual([1]);
  });

  test("加载中途又有人来要同一个模型：共用请求，也能收到进度", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => (release = resolve));
    let reads = 0;
    const slow = async (model: FigureModel, report?: (fraction: number) => void) => {
      reads++;
      report?.(0.5);
      await gate;
      return readModel(model);
    };
    const first: number[] = [];
    const second: number[] = [];
    const a = loadFigureModel(slow, "male", (f) => first.push(f));
    const b = loadFigureModel(slow, "male", (f) => second.push(f));
    release();
    await Promise.all([a, b]);
    expect(reads).toBe(1);
    expect(second.length).toBeGreaterThan(0);
    expect(second.at(-1)).toBe(1);
    expect(first.at(-1)).toBe(1);
  });

  test("加载失败不会上报 1", async () => {
    const seen: number[] = [];
    await loadFigureModel(
      () => Promise.reject(new Error("断网")),
      "male",
      (f) => seen.push(f),
    );
    expect(seen).not.toContain(1);
  });

  test("多个模型的总进度是各自进度的平均", async () => {
    const seen: number[] = [];
    await loadFigureModels(["male", "female"], reporting, (f) => seen.push(f));
    expect(seen).toEqual([...seen].sort((a, b) => a - b));
    expect(seen.at(-1)).toBe(1);
  });
});

describe("读取模型文件并报告下载进度", () => {
  const streamOf = (chunks: number[][], headers: Record<string, string>) =>
    new Response(
      new ReadableStream<Uint8Array>({
        start(controller) {
          for (const chunk of chunks) controller.enqueue(new Uint8Array(chunk));
          controller.close();
        },
      }),
      { headers },
    );

  test("按 Content-Length 边读边报，读完是 1，内容完整", async () => {
    const seen: number[] = [];
    const buffer = await readWithProgress(
      streamOf(
        [
          [1, 2],
          [3, 4],
          [5, 6],
        ],
        { "Content-Length": "6" },
      ),
      (f) => seen.push(f),
    );
    expect([...new Uint8Array(buffer)]).toEqual([1, 2, 3, 4, 5, 6]);
    expect(seen.at(-1)).toBe(1);
    expect(seen.slice(0, -1).every((f) => f < 1)).toBe(true);
    expect(seen).toEqual([...seen].sort((a, b) => a - b));
  });

  test("没有 Content-Length 时读完再报 1", async () => {
    const seen: number[] = [];
    const buffer = await readWithProgress(streamOf([[7, 8, 9]], {}), (f) => seen.push(f));
    expect([...new Uint8Array(buffer)]).toEqual([7, 8, 9]);
    expect(seen).toEqual([1]);
  });

  test("压缩传输时实际字节比 Content-Length 多：进度不会超过 1", async () => {
    const seen: number[] = [];
    await readWithProgress(
      streamOf(
        [
          [1, 2, 3, 4],
          [5, 6, 7, 8],
        ],
        { "Content-Length": "4" },
      ),
      (f) => seen.push(f),
    );
    expect(seen.every((f) => f <= 1)).toBe(true);
    expect(seen.slice(0, -1).every((f) => f < 1)).toBe(true);
  });

  test("不要进度时直接读完", async () => {
    const buffer = await readWithProgress(streamOf([[1, 2, 3]], { "Content-Length": "3" }));
    expect([...new Uint8Array(buffer)]).toEqual([1, 2, 3]);
  });
});

for (const model of MODELS) {
  const name = model === "male" ? "男性" : "女性";
  const figure = (pose: PoseId) => createFigure({ pose, color: "#d4d4d8", model }).group;

  describe(`${name}人台的朝向、身高与落地`, () => {
    beforeEach(async () => {
      await loadFigureModel(readModel, model);
    });

    test("站立时身高是设定的身高，脚踩在地面上", () => {
      const box = measureFigure(figure("stand"));
      expect(box.min.y).toBeCloseTo(0, 4);
      expect(box.max.y).toBeCloseTo(FIGURE_HEIGHT[model], 1);
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
        expect(box.min.y, pose).toBeCloseTo(0, 4);
        expect(box.max.y, pose).toBeGreaterThan(0.2);
      }
    });

    test("换颜色只改自己的材质，不影响别的人物", () => {
      const a = createFigure({ pose: "stand", color: "#ff0000", model });
      const b = createFigure({ pose: "stand", color: "#0000ff", model });
      expect(a.material).not.toBe(b.material);
      expect(a.material.color.getHexString()).toBe("ff0000");
      expect(b.material.color.getHexString()).toBe("0000ff");
    });
  });

  describe(`${name}的 12 个姿势特征`, () => {
    beforeEach(async () => {
      await loadFigureModel(readModel, model);
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
      expect(Math.abs(bone(walk, "foot_l").z - bone(walk, "foot_r").z)).toBeGreaterThan(0.25);
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
      // 女性手臂比男性短约一成，所以阈值按较短的算
      expect(hand.z).toBeLessThan(shoulder.z - 0.35);
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
}

describe("包围盒", () => {
  test("measureFigure 返回人物局部坐标里的包围盒", async () => {
    await loadFigureModel(readModel);
    const box = measureFigure(createFigure({ pose: "stand", color: "#d4d4d8" }).group);
    expect(box).toBeInstanceOf(Box3);
    expect(box.getSize(new Vector3()).y).toBeCloseTo(FIGURE_HEIGHT.male, 1);
  });

  test("女性比男性矮一些", () => {
    expect(FIGURE_HEIGHT.female).toBeLessThan(FIGURE_HEIGHT.male);
  });
});
