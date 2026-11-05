import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import type { SourcePreflight } from "@/api/drama/type";
import { SOURCE_MAX_CHARS, checkSourceText } from "@/utils/drama/options";
import {
  MIN_MODEL_CONTEXT,
  START_RATIOS,
  buildCreateInput,
  durationLabel,
  episodesLabel,
  loadStartPrefs,
  modelDisabledReason,
  normalizeRangeEnd,
  promptsToChoices,
  reconcilePrompts,
  saveStartPrefs,
  secondsLock,
  startBlockReason,
  unitWord,
  type StartForm,
} from "@/utils/drama/start-settings";

const preflight = (patch: Partial<SourcePreflight> = {}): SourcePreflight => ({
  sourceToken: "tok",
  filename: "霜刃.docx",
  chars: 4860000,
  kindGuess: "novel",
  kindSignals: "找到 1,215 个“第N章”",
  unitKind: "chapter",
  units: [],
  unitsTotal: 1215,
  issues: [],
  estimate: {
    episodesMin: 900,
    episodesMax: 1400,
    episodesExact: false,
    credits: 3200,
    minutes: 40,
  },
  ...patch,
});

const form = (patch: Partial<StartForm> = {}): StartForm => ({
  kind: "novel",
  rangeEnd: 0,
  ratio: "9:16",
  genre: "",
  styleKey: "xianxia",
  styleCustom: "",
  seconds: 60,
  textModel: "",
  videoModel: "",
  imageModels: { character: "", scene: "", prop: "" },
  prompts: {
    adapt: { source: "builtin" },
    assets: { source: "builtin" },
    asset_prompts: { source: "builtin" },
    video: { source: "builtin" },
  },
  ...patch,
});

describe("字数上限改 500 万", () => {
  test("SOURCE_MAX_CHARS = 5,000,000，粘贴超出报「500 万字」", () => {
    expect(SOURCE_MAX_CHARS).toBe(5_000_000);
    expect(checkSourceText("字".repeat(SOURCE_MAX_CHARS))).toBeNull();
    expect(checkSourceText("字".repeat(SOURCE_MAX_CHARS + 1))).toContain("500 万字");
  });
});

describe("开拍设置：画幅与单位", () => {
  test("画幅只有 9:16 / 16:9 / 4:3 三档", () => {
    expect(START_RATIOS.map((r) => r.key)).toEqual(["9:16", "16:9", "4:3"]);
  });

  test("单元用词：章 / 集 / 场 / 段", () => {
    expect(unitWord("chapter")).toBe("章");
    expect(unitWord("episode")).toBe("集");
    expect(unitWord("scene")).toBe("场");
    expect(unitWord("chunk")).toBe("段");
  });

  test("范围终点归一到 1..单元数，非法值回落 1", () => {
    expect(normalizeRangeEnd(0, 1215)).toBe(1);
    expect(normalizeRangeEnd(5000, 1215)).toBe(1215);
    expect(normalizeRangeEnd(120.7, 1215)).toBe(120);
    expect(normalizeRangeEnd(Number.NaN, 1215)).toBe(1);
  });

  test("预估文案：集数区间 / 精确、耗时分钟 / 小时", () => {
    expect(episodesLabel({ episodesMin: 900, episodesMax: 1400, episodesExact: false })).toBe(
      "900–1,400",
    );
    expect(episodesLabel({ episodesMin: 80, episodesMax: 80, episodesExact: true })).toBe("80");
    expect(durationLabel(0.4)).toBe("1 分钟");
    expect(durationLabel(40)).toBe("40 分钟");
    expect(durationLabel(90)).toBe("1.5 小时");
  });
});

describe("开拍设置：剧本型输入的规则", () => {
  test("剧本带「第N集」：单集时长锁成「按原分集 · N 集」；无分集标记的剧本只作参考", () => {
    expect(secondsLock("script", preflight({ unitKind: "episode", unitsTotal: 80 }))).toEqual({
      locked: true,
      label: "按原分集 · 80 集",
    });
    expect(secondsLock("script", preflight({ unitKind: "scene", unitsTotal: 30 }))).toEqual({
      locked: false,
      label: null,
    });
    expect(secondsLock("novel", preflight({ unitKind: "episode", unitsTotal: 80 })).locked).toBe(
      false,
    );
  });

  test("上下文低于 128k 的模型禁用并写原因；未知上下文不拦", () => {
    expect(MIN_MODEL_CONTEXT).toBe(128_000);
    expect(modelDisabledReason(64_000)).toBe("上下文低于 128k，装不下一批完整原文");
    expect(modelDisabledReason(127_999)).not.toBeNull();
    expect(modelDisabledReason(128_000)).toBeNull();
    expect(modelDisabledReason(1_000_000)).toBeNull();
    expect(modelDisabledReason(undefined)).toBeNull();
    expect(modelDisabledReason(0)).toBeNull();
  });

  test("识别不出且没选类型时开始禁用；选了就放行", () => {
    expect(startBlockReason({ kind: null, styleKey: "x", modelBlocked: false })).toBe(
      "先确认是剧本还是小说",
    );
    expect(startBlockReason({ kind: "novel", styleKey: "x", modelBlocked: false })).toBeNull();
    expect(startBlockReason({ kind: "novel", styleKey: "", modelBlocked: false })).toBe(
      "先选一个画风",
    );
    expect(startBlockReason({ kind: "novel", styleKey: "x", modelBlocked: true })).toBe(
      "所选模型上下文低于 128k，请换一个",
    );
  });
});

describe("开拍设置：建剧请求", () => {
  test("小说：带 token、类型、范围、画幅、风格、四环节提示词", () => {
    const body = buildCreateInput(
      preflight(),
      form({
        rangeEnd: 120,
        ratio: "16:9",
        genre: "玄幻修仙",
        seconds: 90,
        textModel: "gpt",
        prompts: {
          adapt: { source: "mine", id: "1f" },
          assets: { source: "skill", skill: "cross" },
          asset_prompts: { source: "builtin" },
          video: { source: "builtin" },
        },
      }),
    );
    expect(body).toEqual({
      sourceToken: "tok",
      sourceKind: "novel",
      rangeEnd: 120,
      ratio: "16:9",
      genre: "玄幻修仙",
      styleKey: "xianxia",
      styleCustom: undefined,
      episodeSeconds: 90,
      textModel: "gpt",
      prompts: {
        adapt: { source: "mine", id: "1f" },
        assets: { source: "skill", skill: "cross" },
        asset_prompts: { source: "builtin" },
        video: { source: "builtin" },
      },
    });
  });

  test("剧本型：剧本改编强制内置，规划范围恒为全部；自定义画风带描述", () => {
    const body = buildCreateInput(
      preflight({ kindGuess: "script" }),
      form({
        kind: "script",
        rangeEnd: 30,
        styleKey: "custom",
        styleCustom: " 黏土 ",
        prompts: {
          adapt: { source: "mine", id: "1f" },
          assets: { source: "builtin" },
          asset_prompts: { source: "builtin" },
          video: { source: "builtin" },
        },
      }),
    );
    expect(body.sourceKind).toBe("script");
    expect(body.prompts?.adapt).toEqual({ source: "builtin" });
    expect(body.rangeEnd).toBe(0);
    expect(body.styleCustom).toBe("黏土");
  });

  test("提示词选择：选了内置的环节也显式带上 builtin", () => {
    expect(
      promptsToChoices({
        adapt: { source: "builtin" },
        assets: { source: "builtin" },
        asset_prompts: { source: "builtin" },
        video: { source: "builtin" },
      }).video,
    ).toEqual({ source: "builtin" });
  });
});

describe("开拍设置：记住上次选择", () => {
  const store = new Map<string, string>();
  const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  beforeEach(() => {
    store.clear();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (k: string) => store.get(k) ?? null,
        setItem: (k: string, v: string) => void store.set(k, v),
        removeItem: (k: string) => void store.delete(k),
      },
    });
  });
  afterEach(() => {
    if (original) Object.defineProperty(globalThis, "localStorage", original);
    else delete (globalThis as { localStorage?: unknown }).localStorage;
  });

  test("存取画幅、题材、画风、时长、模型、提示词；规划范围不记", () => {
    saveStartPrefs(form({ rangeEnd: 120, ratio: "4:3", genre: "古装", seconds: 120 }));
    const prefs = loadStartPrefs();
    expect(prefs).toMatchObject({ ratio: "4:3", genre: "古装", seconds: 120, styleKey: "xianxia" });
    expect(prefs && "rangeEnd" in prefs).toBe(false);
  });

  test("损坏的存储当作没有；非法画幅回落 9:16", () => {
    store.set("drama-start-prefs", "{oops");
    expect(loadStartPrefs()).toBeNull();
    store.set("drama-start-prefs", JSON.stringify({ ratio: "21:9", seconds: 999 }));
    expect(loadStartPrefs()).toMatchObject({ ratio: "9:16", seconds: 60 });
  });

  test("localStorage 抛错时不影响弹窗", () => {
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      get() {
        throw new Error("blocked");
      },
    });
    expect(loadStartPrefs()).toBeNull();
    expect(() => saveStartPrefs(form())).not.toThrow();
  });

  test("被删的我的提示词 / 停用的技能回落为内置", () => {
    const next = reconcilePrompts(
      {
        adapt: { source: "mine", id: "gone" },
        assets: { source: "skill", skill: "off" },
        asset_prompts: { source: "mine", id: "1f" },
        video: { source: "skill", skill: "on" },
      },
      {
        adapt: [],
        assets: [],
        asset_prompts: [{ id: "1f" }],
        video: [],
      },
      { adapt: [], assets: [], asset_prompts: [], video: [{ name: "on" }] },
    );
    expect(next.adapt).toEqual({ source: "builtin" });
    expect(next.assets).toEqual({ source: "builtin" });
    expect(next.asset_prompts).toEqual({ source: "mine", id: "1f" });
    expect(next.video).toEqual({ source: "skill", skill: "on" });
  });
});

describe("开拍设置：视频 / 图片模型", () => {
  const store = new Map<string, string>();
  const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  beforeEach(() => {
    store.clear();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (k: string) => store.get(k) ?? null,
        setItem: (k: string, v: string) => void store.set(k, v),
      },
    });
  });
  afterEach(() => {
    if (original) Object.defineProperty(globalThis, "localStorage", original);
    else delete (globalThis as { localStorage?: unknown }).localStorage;
  });

  test("记住的选择往返：视频模型与三个图片模型", () => {
    saveStartPrefs(
      form({ videoModel: "v1", imageModels: { character: "c1", scene: "s1", prop: "p1" } }),
    );
    const prefs = loadStartPrefs();
    expect(prefs?.videoModel).toBe("v1");
    expect(prefs?.imageModels).toEqual({ character: "c1", scene: "s1", prop: "p1" });
  });

  test("旧存档没有这些字段：回落为空串", () => {
    localStorage.setItem("drama-start-prefs", JSON.stringify({ textModel: "m", styleKey: "x" }));
    const prefs = loadStartPrefs();
    expect(prefs?.videoModel).toBe("");
    expect(prefs?.imageModels).toEqual({ character: "", scene: "", prop: "" });
  });

  test("字段类型不对：逐项回落为空串，不影响其他项", () => {
    localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({ videoModel: 3, imageModels: { character: "c1", scene: 5, prop: null } }),
    );
    const prefs = loadStartPrefs();
    expect(prefs?.videoModel).toBe("");
    expect(prefs?.imageModels).toEqual({ character: "c1", scene: "", prop: "" });
    localStorage.setItem("drama-start-prefs", JSON.stringify({ imageModels: "oops" }));
    expect(loadStartPrefs()?.imageModels).toEqual({ character: "", scene: "", prop: "" });
  });

  test("建剧请求：带上选了的模型，空串不发", () => {
    const input = buildCreateInput(
      preflight(),
      form({ videoModel: "v1", imageModels: { character: "c1", scene: "", prop: "p1" } }),
    );
    expect(input.videoModel).toBe("v1");
    expect(input.imageModels?.character).toBe("c1");
    expect(input.imageModels?.prop).toBe("p1");
    expect(input.imageModels?.scene).toBeUndefined();
    const bare = buildCreateInput(preflight(), form());
    expect(bare.videoModel).toBeUndefined();
    expect(bare.imageModels).toBeUndefined();
  });
});
