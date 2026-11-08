import { describe, expect, test } from "bun:test";

import {
  GREET_COOLDOWN_MS,
  IDLE_SLEEP_MS,
  canGreet,
  nextReaction,
  petMood,
  reactionSpan,
  type PetMoodInput,
} from "@/utils/pet/mood";
import { PET_ROW } from "@/utils/pet/rows";

const base: PetMoodInput = {
  dragging: false,
  dragDir: null,
  reaction: null,
  now: 1_000_000,
  pending: false,
  busy: false,
  runningTasks: 0,
  available: true,
  idleMs: 0,
  hovering: false,
  mode: "lively",
  reducedMotion: false,
  hasSleeping: true,
};
const mood = (patch: Partial<PetMoodInput>) => petMood({ ...base, ...patch });

describe("petMood 优先级", () => {
  test("什么都没有：idle 循环", () => {
    expect(mood({})).toEqual({ row: PET_ROW.idle, loop: true, sleepFallback: false });
  });

  test("1 拖动：按方向选 running-left / running-right，几乎不动用 idle", () => {
    expect(mood({ dragging: true, dragDir: "left" }).row).toBe(PET_ROW["running-left"]);
    expect(mood({ dragging: true, dragDir: "right" }).row).toBe(PET_ROW["running-right"]);
    expect(mood({ dragging: true, dragDir: null }).row).toBe(PET_ROW.idle);
  });

  test("拖动压过一切，包括一次性反应和待审批", () => {
    const r = mood({
      dragging: true,
      dragDir: "left",
      reaction: { kind: "celebrate", until: base.now + 500 },
      pending: true,
      busy: true,
    });
    expect(r.row).toBe(PET_ROW["running-left"]);
  });

  test("2 一次性反应：celebrate 播 jumping、failed 播 failed，只播一轮", () => {
    const until = base.now + 1000;
    expect(mood({ reaction: { kind: "celebrate", until } })).toEqual({
      row: PET_ROW.jumping,
      loop: false,
      sleepFallback: false,
    });
    expect(mood({ reaction: { kind: "failed", until } }).row).toBe(PET_ROW.failed);
  });

  test("反应压过待审批和助手运行；过期后回到下一级", () => {
    const live = { kind: "celebrate" as const, until: base.now + 1 };
    expect(mood({ reaction: live, pending: true, busy: true }).row).toBe(PET_ROW.jumping);
    expect(mood({ reaction: { ...live, until: base.now }, pending: true }).row).toBe(
      PET_ROW.waiting,
    );
  });

  test("3 待审批 waiting 压过助手运行", () => {
    expect(mood({ pending: true, busy: true }).row).toBe(PET_ROW.waiting);
  });

  test("4 助手运行 running 压过画布生成中", () => {
    expect(mood({ busy: true, runningTasks: 3 }).row).toBe(PET_ROW.running);
  });

  test("5 画布有生成任务：review", () => {
    expect(mood({ runningTasks: 1 }).row).toBe(PET_ROW.review);
  });

  test("6 没有可用模型：sleeping；模型还在加载（null）不睡", () => {
    expect(mood({ available: false }).row).toBe(PET_ROW.sleeping);
    expect(mood({ available: null }).row).toBe(PET_ROW.idle);
  });

  test("6 有任务在跑时，即使没有可用模型也是 review（按优先级）", () => {
    expect(mood({ available: false, runningTasks: 2 }).row).toBe(PET_ROW.review);
  });

  test("6 活泼档空闲满 10 分钟睡着，没满不睡", () => {
    expect(mood({ idleMs: IDLE_SLEEP_MS }).row).toBe(PET_ROW.sleeping);
    expect(mood({ idleMs: IDLE_SLEEP_MS - 1 }).row).toBe(PET_ROW.idle);
  });

  test("安静档：不因空闲睡着，但没有可用模型照睡", () => {
    expect(mood({ mode: "quiet", idleMs: IDLE_SLEEP_MS * 3 }).row).toBe(PET_ROW.idle);
    expect(mood({ mode: "quiet", available: false }).row).toBe(PET_ROW.sleeping);
  });

  test("7 悬停：活泼档打招呼播一次，安静档不打招呼", () => {
    expect(mood({ hovering: true })).toEqual({
      row: PET_ROW.waving,
      loop: false,
      sleepFallback: false,
    });
    expect(mood({ hovering: true, mode: "quiet" }).row).toBe(PET_ROW.idle);
  });

  test("悬停打招呼排在睡着之后：睡着时悬停仍是 sleeping", () => {
    expect(mood({ hovering: true, idleMs: IDLE_SLEEP_MS }).row).toBe(PET_ROW.sleeping);
  });

  test("悬停打招呼排在助手运行之后", () => {
    expect(mood({ hovering: true, busy: true }).row).toBe(PET_ROW.running);
  });

  test("没有 sleeping 行：回退到 idle，并单独标出交给渲染层，不循环", () => {
    expect(mood({ available: false, hasSleeping: false })).toEqual({
      row: PET_ROW.idle,
      loop: false,
      sleepFallback: true,
    });
    expect(mood({ idleMs: IDLE_SLEEP_MS, hasSleeping: false }).sleepFallback).toBe(true);
    // 没睡着时不带回退标记
    expect(mood({ hasSleeping: false }).sleepFallback).toBe(false);
  });

  test("减少动态效果：所有状态都不循环", () => {
    expect(mood({ reducedMotion: true }).loop).toBe(false);
    expect(mood({ reducedMotion: true, busy: true }).loop).toBe(false);
    expect(mood({ reducedMotion: true, available: false }).loop).toBe(false);
    expect(mood({ reducedMotion: true, busy: true }).row).toBe(PET_ROW.running);
  });

  test("循环状态：running、waiting、review、sleeping 都循环", () => {
    expect(mood({ busy: true }).loop).toBe(true);
    expect(mood({ pending: true }).loop).toBe(true);
    expect(mood({ runningTasks: 1 }).loop).toBe(true);
    expect(mood({ available: false }).loop).toBe(true);
  });
});

describe("一次性反应", () => {
  test("reactionSpan = 该行一轮时长 + 600ms 停留", () => {
    // jumping：140×4 + 280 = 840；failed：140×7 + 240 = 1220
    expect(reactionSpan("celebrate")).toBe(840 + 600);
    expect(reactionSpan("failed")).toBe(1220 + 600);
  });

  test("没有反应时开始新的，until = now + span", () => {
    expect(nextReaction(null, "celebrate", 100)).toEqual({
      kind: "celebrate",
      until: 100 + reactionSpan("celebrate"),
    });
  });

  test("播放期间又来同类反应：保持原样，不重新开始", () => {
    const cur = nextReaction(null, "celebrate", 100);
    expect(nextReaction(cur, "celebrate", 500)).toBe(cur);
  });

  test("已播完再来同类、或来了不同类：重新开始", () => {
    const cur = nextReaction(null, "celebrate", 100);
    const after = cur.until + 1;
    expect(nextReaction(cur, "celebrate", after).until).toBe(after + reactionSpan("celebrate"));
    expect(nextReaction(cur, "failed", 500).kind).toBe("failed");
  });
});

describe("打招呼冷却", () => {
  test("从没打过、或距上次满 10 秒才能再打", () => {
    expect(canGreet(null, 5)).toBe(true);
    expect(canGreet(1000, 1000 + GREET_COOLDOWN_MS - 1)).toBe(false);
    expect(canGreet(1000, 1000 + GREET_COOLDOWN_MS)).toBe(true);
  });
});
