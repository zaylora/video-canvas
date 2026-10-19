import { describe, expect, test } from "bun:test";

import type { DirectorScene } from "@/types";
import {
  DIRECTOR_LIMITS,
  MAX_SCENE_BYTES,
  addCamera,
  addCharacter,
  addProp,
  canAdd,
  cloneScene,
  createDefaultScene,
  frameCount,
  normalizeScene,
  removeObject,
  sceneSizeError,
  setDuration,
} from "@/utils/director/scene";

/** 按顺序发 id，测试里可预期 */
const ids = (prefix = "id") => {
  let n = 0;
  return () => `${prefix}${++n}`;
};

describe("默认场景", () => {
  test("开箱即有一个人物、一台机位、5 秒", () => {
    const scene = createDefaultScene(ids());
    expect(scene.version).toBe(1);
    expect(scene.duration).toBe(5);
    expect(scene.characters).toHaveLength(1);
    expect(scene.cameras).toHaveLength(1);
    expect(scene.props).toHaveLength(0);
    const [character] = scene.characters;
    const [camera] = scene.cameras;
    expect(character.name).toBe("角色A");
    expect(character.pose).toBe("stand");
    expect(camera.name).toBe("机位1");
    expect(scene.activeCameraId).toBe(camera.id);
  });

  test("机位在人物正前方、看向人物", () => {
    const { characters, cameras } = createDefaultScene(ids());
    expect(cameras[0]).toMatchObject({ x: 0, y: 1.6, z: 4, yaw: 0, fov: 50, aspect: "16:9" });
    expect(cameras[0].lookAt).toBe(characters[0].id);
    // 人物面向机位（朝 +z）
    expect(characters[0]).toMatchObject({ x: 0, z: 0, yaw: 180 });
  });
});

describe("添加与上限", () => {
  test("新人物按字母顺延命名、颜色各不相同", () => {
    let scene = createDefaultScene(ids());
    scene = addCharacter(scene, ids("c")).scene;
    scene = addCharacter(scene, ids("d")).scene;
    expect(scene.characters.map((c) => c.name)).toEqual(["角色A", "角色B", "角色C"]);
    expect(new Set(scene.characters.map((c) => c.color)).size).toBe(3);
  });

  test("删掉中间的人物后，新人物补上空出来的名字", () => {
    let scene = createDefaultScene(ids());
    const b = addCharacter(scene, () => "b");
    scene = addCharacter(b.scene, () => "c").scene;
    scene = removeObject(scene, "b");
    expect(addCharacter(scene, () => "x").scene.characters.map((c) => c.name)).toEqual([
      "角色A",
      "角色C",
      "角色B",
    ]);
  });

  test("新机位成为当前机位，名字按序号顺延", () => {
    const added = addCamera(createDefaultScene(ids()), () => "cam2");
    expect(added.id).toBe("cam2");
    expect(added.scene.activeCameraId).toBe("cam2");
    expect(added.scene.cameras[1].name).toBe("机位2");
  });

  test("添加机位可以指定位置（从当前视角建机位）", () => {
    const added = addCamera(createDefaultScene(ids()), () => "cam2", {
      x: 1,
      y: 2,
      z: 3,
      yaw: 45,
      pitch: -10,
      fov: 40,
    });
    expect(added.scene.cameras[1]).toMatchObject({
      x: 1,
      y: 2,
      z: 3,
      yaw: 45,
      pitch: -10,
      fov: 40,
    });
  });

  test("道具默认是 1 米立方体，平面默认是薄板", () => {
    const box = addProp(createDefaultScene(ids()), () => "p1").scene.props[0];
    expect(box).toMatchObject({ shape: "box", w: 1, h: 1, d: 1, name: "立方体1" });
    const plane = addProp(createDefaultScene(ids()), () => "p2", "plane").scene.props[0];
    expect(plane.shape).toBe("plane");
    expect(plane.d).toBeLessThan(0.1);
  });

  test("到上限就不能再加", () => {
    let scene = createDefaultScene(ids());
    for (let i = scene.characters.length; i < DIRECTOR_LIMITS.characters; i++) {
      scene = addCharacter(scene, ids(`c${i}-`)).scene;
    }
    expect(canAdd(scene, "character")).toBe(false);
    expect(canAdd(scene, "camera")).toBe(true);
    expect(() => addCharacter(scene, ids("x"))).toThrow();
  });
});

describe("删除", () => {
  test("删当前机位，当前机位换成剩下的第一台；删光为空串", () => {
    let scene = addCamera(createDefaultScene(ids()), () => "cam2").scene;
    scene = removeObject(scene, "cam2");
    expect(scene.activeCameraId).toBe(scene.cameras[0].id);
    scene = removeObject(scene, scene.cameras[0].id);
    expect(scene.activeCameraId).toBe("");
  });

  test("删人物时清掉看向它的机位引用", () => {
    const scene = createDefaultScene(ids());
    const next = removeObject(scene, scene.characters[0].id);
    expect(next.cameras[0].lookAt).toBeUndefined();
  });
});

describe("时长", () => {
  test("限制在 1–15 秒、按 0.5 取整，已有关键帧按比例缩放", () => {
    const scene: DirectorScene = {
      ...createDefaultScene(ids()),
      duration: 4,
    };
    scene.cameras[0].keys = [
      { ...scene.cameras[0], t: 0, ease: "smooth" },
      { ...scene.cameras[0], t: 4, ease: "smooth" },
    ];
    const next = setDuration(scene, 8.2);
    expect(next.duration).toBe(8);
    expect(next.cameras[0].keys.map((k) => k.t)).toEqual([0, 8]);
    expect(setDuration(scene, 0.2).duration).toBe(1);
    expect(setDuration(scene, 99).duration).toBe(15);
  });

  test("帧数按 24fps", () => {
    expect(frameCount(5)).toBe(120);
    expect(frameCount(2.5)).toBe(60);
  });
});

describe("复制", () => {
  test("所有 id 换新，引用跟着换", () => {
    const scene = createDefaultScene(ids());
    const copy = cloneScene(scene, ids("n"));
    const oldIds = [...scene.characters, ...scene.cameras].map((o) => o.id);
    const newIds = [...copy.characters, ...copy.cameras].map((o) => o.id);
    expect(newIds.some((id) => oldIds.includes(id))).toBe(false);
    expect(copy.activeCameraId).toBe(copy.cameras[0].id);
    expect(copy.cameras[0].lookAt).toBe(copy.characters[0].id);
    // 原场景不受影响
    expect(scene.cameras[0].lookAt).toBe(scene.characters[0].id);
  });
});

describe("体积校验", () => {
  test("正常场景通过，超过上限给出原因", () => {
    const scene = createDefaultScene(ids());
    expect(sceneSizeError(scene)).toBeNull();
    const huge = {
      ...scene,
      characters: [{ ...scene.characters[0], name: "x".repeat(MAX_SCENE_BYTES) }],
    };
    expect(sceneSizeError(huge)).toContain("过大");
  });
});

describe("读档校验", () => {
  test("合法场景原样返回", () => {
    const scene = createDefaultScene(ids());
    expect(normalizeScene(JSON.parse(JSON.stringify(scene)))).toEqual(scene);
  });

  test("不是场景的值返回 undefined", () => {
    expect(normalizeScene(undefined)).toBeUndefined();
    expect(normalizeScene({ version: 2 })).toBeUndefined();
    expect(normalizeScene({ version: 1, duration: 5 })).toBeUndefined();
  });

  test("关键帧乱序时按时间排好", () => {
    const scene = createDefaultScene(ids());
    const raw = JSON.parse(JSON.stringify(scene));
    raw.cameras[0].keys = [
      { ...raw.cameras[0], t: 3, ease: "smooth" },
      { ...raw.cameras[0], t: 1, ease: "smooth" },
    ];
    expect(normalizeScene(raw)?.cameras[0].keys.map((k) => k.t)).toEqual([1, 3]);
  });
});
