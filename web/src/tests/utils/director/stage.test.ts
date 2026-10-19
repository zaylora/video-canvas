import { afterEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { PerspectiveCamera, Vector3 } from "three";

import type { DirectorScene } from "@/types";
import { loadFigureModel, resetFigureModel } from "@/utils/director/figure";
import { sampleCamera, sampleCharacter } from "@/utils/director/sample";
import { addCamera, addProp, createDefaultScene, removeObject } from "@/utils/director/scene";
import { applyCameraPose, createStage } from "@/utils/director/stage";

const scene = (): DirectorScene => {
  let n = 0;
  return createDefaultScene(() => `id${++n}`);
};

const poseAt =
  (s: DirectorScene, t = 0) =>
  (id: string) => {
    const character = s.characters.find((c) => c.id === id);
    if (character) return sampleCharacter(character, t);
    const camera = s.cameras.find((c) => c.id === id);
    return camera ? sampleCamera(camera, t) : undefined;
  };

afterEach(() => resetFigureModel());

describe("舞台同步", () => {
  test("按场景建出人物、道具和机位", () => {
    const stage = createStage({ helpers: true });
    const s = addProp(scene(), () => "p1").scene;
    stage.sync(s, poseAt(s));
    expect(stage.objectOf("id1")).toBeDefined();
    expect(stage.objectOf("p1")).toBeDefined();
    expect(stage.objectOf("id2")).toBeDefined();
  });

  test("干净舞台（导出、缩略图用）不放机位", () => {
    const stage = createStage({ helpers: false });
    const s = scene();
    stage.sync(s, poseAt(s));
    expect(stage.objectOf("id2")).toBeUndefined();
    expect(stage.guides.visible).toBe(false);
  });

  test("删掉的对象从舞台上移除", () => {
    const stage = createStage({ helpers: true });
    let s = addProp(scene(), () => "p1").scene;
    stage.sync(s, poseAt(s));
    s = removeObject(s, "p1");
    stage.sync(s, poseAt(s));
    expect(stage.objectOf("p1")).toBeUndefined();
  });

  test("人物按站位摆放、按偏航转向", () => {
    const stage = createStage({ helpers: true });
    const s = scene();
    s.characters[0] = { ...s.characters[0], x: 2, z: -1, yaw: 90 };
    stage.sync(s, poseAt(s));
    const figure = stage.objectOf("id1")!;
    expect(figure.position.x).toBe(2);
    expect(figure.position.z).toBe(-1);
    // 偏航 90° 时面朝 +x：本地 -z 方向转到世界 +x
    const front = new Vector3(0, 0, -1).applyQuaternion(figure.quaternion);
    expect(front.x).toBeCloseTo(1, 6);
  });

  test("道具尺寸跟着场景变", () => {
    const stage = createStage({ helpers: true });
    const s = addProp(scene(), () => "p1").scene;
    s.props[0] = { ...s.props[0], w: 3, h: 2 };
    stage.sync(s, poseAt(s));
    const box = new Vector3();
    stage.objectOf("p1")!.updateMatrixWorld(true);
    stage.objectOf("p1")!.children[0].getWorldScale(box);
    expect(box.x).toBeCloseTo(3, 6);
    expect(box.y).toBeCloseTo(2, 6);
  });

  test("姿势变了才重建人物", () => {
    const stage = createStage({ helpers: true });
    const s = scene();
    stage.sync(s, poseAt(s));
    const before = stage.objectOf("id1");
    stage.sync(s, poseAt(s));
    expect(stage.objectOf("id1")).toBe(before);
    s.characters[0] = { ...s.characters[0], pose: "sit" };
    stage.sync(s, poseAt(s));
    expect(stage.objectOf("id1")).not.toBe(before);
  });

  test("人台模型加载完，已经画出来的简化形体自动换成人台", async () => {
    const stage = createStage({ helpers: true });
    const s = scene();
    stage.sync(s, poseAt(s));
    const before = stage.objectOf("id1")!;
    expect(before.getObjectByName("hand_l")).toBeUndefined();
    const bytes = readFileSync(
      new URL("../../../../public/director/mannequin.glb", import.meta.url),
    );
    await loadFigureModel(
      async () =>
        bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer,
    );
    stage.sync(s, poseAt(s));
    const after = stage.objectOf("id1")!;
    expect(after).not.toBe(before);
    expect(after.getObjectByName("hand_l")).toBeDefined();
    expect(after.position.x).toBe(0);
  });

  test("点中人物的任一部件都能反查到人物 id", () => {
    const stage = createStage({ helpers: true });
    const s = scene();
    stage.sync(s, poseAt(s));
    const part = stage.objectOf("id1")!.children[0];
    expect(stage.idOf(part)).toBe("id1");
    expect(stage.pickables()).toContain(stage.objectOf("id1")!);
  });

  test("机位的前方对准它的偏航和俯仰", () => {
    const stage = createStage({ helpers: true });
    const s = addCamera(scene(), () => "c2", {
      x: 0,
      y: 1,
      z: 0,
      yaw: 90,
      pitch: 0,
      fov: 40,
    }).scene;
    stage.sync(s, poseAt(s));
    const camera = stage.objectOf("c2")!;
    const front = new Vector3(0, 0, -1).applyQuaternion(camera.quaternion);
    expect(front.x).toBeCloseTo(1, 6);
  });
});

describe("相机套用机位状态", () => {
  test("位置、朝向、视场角和画幅", () => {
    const camera = new PerspectiveCamera();
    applyCameraPose(camera, { x: 1, y: 2, z: 3, yaw: -90, pitch: 30, fov: 35 }, 16 / 9);
    const front = new Vector3();
    camera.getWorldDirection(front);
    expect(camera.position.toArray()).toEqual([1, 2, 3]);
    expect(front.x).toBeCloseTo(-Math.cos(Math.PI / 6), 6);
    expect(front.y).toBeCloseTo(0.5, 6);
    expect(camera.fov).toBe(35);
    expect(camera.aspect).toBeCloseTo(16 / 9, 6);
  });
});
