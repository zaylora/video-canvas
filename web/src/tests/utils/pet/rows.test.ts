import { describe, expect, test } from "bun:test";

import { PET_ROWS, PET_ROW, parsePetManifest, resolvePetRows } from "@/utils/pet/rows";

describe("动作行常量", () => {
  test("共 10 行，行号 0 到 9 依次排列", () => {
    expect(PET_ROWS.map((r) => r.row)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(PET_ROWS.map((r) => r.name)).toEqual([
      "idle",
      "running-right",
      "running-left",
      "waving",
      "jumping",
      "failed",
      "waiting",
      "running",
      "review",
      "sleeping",
    ]);
  });

  test("每行帧数与 hatch-pet 规范一致，帧时长个数等于帧数", () => {
    expect(PET_ROWS.map((r) => r.frames)).toEqual([6, 8, 8, 4, 5, 8, 6, 6, 6, 6]);
    for (const r of PET_ROWS) expect(r.durations.length).toBe(r.frames);
  });

  test("帧时长：idle 与最后一帧停留更久的各行", () => {
    const byName = (n: string) => PET_ROWS.find((r) => r.name === n)!.durations;
    expect(byName("idle")).toEqual([280, 110, 110, 140, 140, 320]);
    expect(byName("running-right")).toEqual([120, 120, 120, 120, 120, 120, 120, 220]);
    expect(byName("waving")).toEqual([140, 140, 140, 280]);
    expect(byName("failed")).toEqual([140, 140, 140, 140, 140, 140, 140, 240]);
    expect(byName("waiting")).toEqual([150, 150, 150, 150, 150, 260]);
    expect(byName("review")).toEqual([150, 150, 150, 150, 150, 280]);
    expect(byName("sleeping")).toEqual([220, 220, 220, 220, 220, 400]);
  });

  test("PET_ROW 按名字查行号", () => {
    expect(PET_ROW.idle).toBe(0);
    expect(PET_ROW.sleeping).toBe(9);
    expect(PET_ROW["running-left"]).toBe(2);
  });
});

describe("解析 pet.json", () => {
  test("读出 Codex 字段与扩展声明", () => {
    const m = parsePetManifest({
      id: "xiaojing",
      displayName: "小镜",
      description: "折耳小黑猫",
      spritesheetPath: "spritesheet.webp",
      extensions: ["sleeping"],
    });
    expect(m).toEqual({
      id: "xiaojing",
      displayName: "小镜",
      description: "折耳小黑猫",
      spritesheetPath: "spritesheet.webp",
      extensions: ["sleeping"],
      rows: {},
    });
  });

  test("可选字段缺省：展示名取 id，描述为空，没有扩展", () => {
    const m = parsePetManifest({ id: "p", spritesheetPath: "a.webp" });
    expect(m.displayName).toBe("p");
    expect(m.description).toBe("");
    expect(m.extensions).toEqual([]);
  });

  test("缺 id 或图集路径、不是对象：抛错", () => {
    expect(() => parsePetManifest(null)).toThrow();
    expect(() => parsePetManifest({ spritesheetPath: "a.webp" })).toThrow();
    expect(() => parsePetManifest({ id: "p" })).toThrow();
  });

  test("extensions 里的非字符串被忽略；rows 里非法的条目被忽略", () => {
    const m = parsePetManifest({
      id: "p",
      spritesheetPath: "a.webp",
      extensions: ["sleeping", 3, null],
      rows: { idle: [100, 200], jumping: [0, 5], nope: [1], waving: "x" },
    });
    expect(m.extensions).toEqual(["sleeping"]);
    expect(m.rows).toEqual({ idle: [100, 200] });
  });
});

describe("按清单解析出实际的行", () => {
  const base = { id: "p", displayName: "p", description: "", spritesheetPath: "a.webp", rows: {} };

  test("声明了 sleeping：10 行", () => {
    const r = resolvePetRows({ ...base, extensions: ["sleeping"] });
    expect(r.rows.length).toBe(10);
    expect(r.hasSleeping).toBe(true);
  });

  test("没声明 sleeping：只有 9 行，标出没有睡觉行", () => {
    const r = resolvePetRows({ ...base, extensions: [] });
    expect(r.rows.length).toBe(9);
    expect(r.rows.some((x) => x.name === "sleeping")).toBe(false);
    expect(r.hasSleeping).toBe(false);
  });

  test("清单里的 rows 覆盖默认帧时长，帧数跟着变", () => {
    const r = resolvePetRows({ ...base, extensions: [], rows: { idle: [100, 100, 300] } });
    const idle = r.rows.find((x) => x.name === "idle")!;
    expect(idle.frames).toBe(3);
    expect(idle.durations).toEqual([100, 100, 300]);
  });
});
