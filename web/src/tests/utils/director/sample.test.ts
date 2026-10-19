import { describe, expect, test } from "bun:test";

import type { CameraKey, CharacterKey, DirectorCamera, DirectorCharacter } from "@/types";
import { sampleCamera, sampleCharacter } from "@/utils/director/sample";

const character = (keys: CharacterKey[] = []): DirectorCharacter => ({
  id: "c",
  name: "角色A",
  color: "#ccc",
  pose: "stand",
  scale: 1,
  x: 9,
  z: 9,
  yaw: 0,
  keys,
});

const camera = (keys: CameraKey[] = []): DirectorCamera => ({
  id: "k",
  name: "机位1",
  aspect: "16:9",
  x: 0,
  y: 1.6,
  z: 4,
  yaw: 0,
  pitch: 0,
  fov: 50,
  keys,
});

const ck = (t: number, x: number, ease: CameraKey["ease"] = "smooth"): CameraKey => ({
  t,
  x,
  y: 1.6,
  z: 4,
  yaw: 0,
  pitch: 0,
  fov: 50,
  ease,
});

describe("人物采样", () => {
  test("没有关键帧用静态站位", () => {
    expect(sampleCharacter(character(), 2)).toEqual({ x: 9, z: 9, yaw: 0 });
  });

  test("只有一帧时全程保持", () => {
    const one = character([{ t: 1, x: 2, z: 3, yaw: 90, ease: "smooth" }]);
    expect(sampleCharacter(one, 0)).toEqual({ x: 2, z: 3, yaw: 90 });
    expect(sampleCharacter(one, 4)).toEqual({ x: 2, z: 3, yaw: 90 });
  });

  test("首帧之前取首帧，末帧之后取末帧", () => {
    const two = character([
      { t: 1, x: 0, z: 0, yaw: 0, ease: "linear" },
      { t: 3, x: 4, z: 0, yaw: 0, ease: "linear" },
    ]);
    expect(sampleCharacter(two, 0).x).toBe(0);
    expect(sampleCharacter(two, 5).x).toBe(4);
  });

  test("朝向按最短方向转：170° 到 -170° 经过 180°", () => {
    const turn = character([
      { t: 0, x: 0, z: 0, yaw: 170, ease: "linear" },
      { t: 2, x: 0, z: 0, yaw: -170, ease: "linear" },
    ]);
    expect(Math.abs(sampleCharacter(turn, 1).yaw)).toBeCloseTo(180, 5);
  });
});

describe("机位采样", () => {
  test("线性插值取中点", () => {
    const cam = camera([ck(0, 0, "linear"), ck(2, 4, "linear")]);
    expect(sampleCamera(cam, 1).x).toBeCloseTo(2, 6);
  });

  test("缓入缓出：中点相同，四分之一处比线性慢", () => {
    const cam = camera([ck(0, 0, "smooth"), ck(4, 4, "smooth")]);
    expect(sampleCamera(cam, 2).x).toBeCloseTo(2, 6);
    expect(sampleCamera(cam, 1).x).toBeLessThan(1);
  });

  test("插值方式由前一帧决定", () => {
    const cam = camera([ck(0, 0, "linear"), ck(2, 2, "smooth"), ck(4, 6, "smooth")]);
    expect(sampleCamera(cam, 1).x).toBeCloseTo(1, 6);
    expect(sampleCamera(cam, 3).x).toBeCloseTo(4, 6);
  });

  test("边界 t=0 与 t=末帧精确命中", () => {
    const cam = camera([ck(0, 1), ck(5, 3)]);
    expect(sampleCamera(cam, 0).x).toBe(1);
    expect(sampleCamera(cam, 5).x).toBe(3);
  });

  test("没有关键帧用静态状态", () => {
    expect(sampleCamera(camera(), 3)).toEqual({ x: 0, y: 1.6, z: 4, yaw: 0, pitch: 0, fov: 50 });
  });
});
