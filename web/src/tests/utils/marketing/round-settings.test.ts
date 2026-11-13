import { describe, expect, test } from "bun:test";

import type { Round, StepRun } from "@/api/marketing/type";
import type { ModelInfo } from "@/api/model/type";
import { roundModelSupport, roundSpec, switchedRoundParams } from "@/utils/marketing/composer";
import { factsJustSucceeded } from "@/utils/marketing/events";

const caps = (patch: Partial<ModelInfo["capabilities"]> = {}): ModelInfo["capabilities"] => ({
  ops: ["omni"],
  refs: {
    image: { on: true, max: 9, max_mb: 10 },
    video: { on: true, max: 1, max_mb: 50 },
    audio: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 4000 },
  params: {
    aspect_ratio: { type: "enum", label: "画幅", open: true, options: ["16:9", "9:16"] },
    resolution: { type: "enum", label: "分辨率", open: true, options: ["480p", "720p", "1080p"] },
    duration: { type: "number", label: "时长", open: true, min: 4, max: 15, step: 1 },
  },
  ...patch,
});

const model = (key: string, patch: Partial<ModelInfo["capabilities"]> = {}): ModelInfo => ({
  key,
  kind: "video",
  label: `模型 ${key}`,
  pricing: { billing: "per_second", per_second: 8 },
  capabilities: caps(patch),
});

const base = { images: 2, hasRef: false, aspectRatio: "9:16", duration: 15 };

describe("mkt: 写完脚本后换出片模型", () => {
  test("读出本轮画幅、分辨率、时长，两种画幅叫法都认", () => {
    expect(roundSpec({ params: { ratio: "9:16", resolution: "768p", duration: 15 } })).toEqual({
      aspectRatio: "9:16",
      resolution: "768p",
      duration: 15,
    });
    expect(roundSpec({ params: {} })).toEqual({
      aspectRatio: undefined,
      resolution: undefined,
      duration: undefined,
    });
  });

  test("支持本轮画幅与时长的模型可选；数值时长按范围与步长判断", () => {
    expect(roundModelSupport(model("a"), base)).toEqual({ ok: true, reason: null });
    expect(roundModelSupport(model("a"), { ...base, duration: 7 }).ok).toBe(true);
  });

  test("不支持本轮画幅、时长、参考视频的模型置灰并写原因", () => {
    const wide = roundModelSupport(model("a"), { ...base, aspectRatio: "1:1" });
    expect(wide.ok).toBe(false);
    expect(wide.reason).toContain("1:1");
    const long = roundModelSupport(model("a"), { ...base, duration: 20 });
    expect(long.ok).toBe(false);
    expect(long.reason).toContain("20");
    const enumDuration = model("b", {
      params: { duration: { type: "enum", label: "时长", open: true, options: [5, 10] } },
    });
    expect(roundModelSupport(enumDuration, { ...base, aspectRatio: undefined }).ok).toBe(false);
    const noVideo = model("c", {
      refs: {
        image: { on: true, max: 9, max_mb: 10 },
        video: { on: false, max: 0, max_mb: 0 },
        audio: { on: false, max: 0, max_mb: 0 },
      },
    });
    expect(roundModelSupport(noVideo, { ...base, hasRef: true }).reason).toContain("参考视频");
    expect(roundModelSupport(model("d", { ops: ["t2v"] }), base).ok).toBe(false);
  });

  test("换模型后画幅、时长照旧，分辨率没有就退回 720p", () => {
    const next = switchedRoundParams(model("a").capabilities, {
      aspectRatio: "9:16",
      resolution: "768p",
      duration: 15,
    });
    expect(next).toEqual({ aspect_ratio: "9:16", resolution: "720p", duration: 15 });
  });

  test("原来没有的画幅、时长不凭空补上", () => {
    const next = switchedRoundParams(model("a").capabilities, { resolution: "1080p" });
    expect(next).toEqual({ resolution: "1080p" });
  });
});

const step = (id: string, status: StepRun["status"]): StepRun =>
  ({ id, step: "facts", status }) as StepRun;
const round = (steps: StepRun[]): Round => ({ id: "r1", steps }) as unknown as Round;

describe("mkt: 事实卡刚成功时重新拉详情", () => {
  test("新一次运行成功、同一次从进行中变成功都算", () => {
    expect(factsJustSucceeded(round([step("1", "reused")]), round([step("2", "succeeded")]))).toBe(
      true,
    );
    expect(factsJustSucceeded(round([step("2", "running")]), round([step("2", "succeeded")]))).toBe(
      true,
    );
    expect(factsJustSucceeded(undefined, round([step("2", "succeeded")]))).toBe(true);
  });

  test("没变化或没成功不拉", () => {
    expect(
      factsJustSucceeded(round([step("2", "succeeded")]), round([step("2", "succeeded")])),
    ).toBe(false);
    expect(factsJustSucceeded(round([]), round([step("2", "running")]))).toBe(false);
    expect(factsJustSucceeded(round([]), round([step("2", "failed")]))).toBe(false);
  });
});
