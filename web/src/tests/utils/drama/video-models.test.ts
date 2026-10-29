import { describe, expect, test } from "bun:test";

import type { Capabilities, ModelInfo, ParamField } from "@/api/model/type";
import {
  fitDuration,
  modelMaxSeconds,
  modelSummary,
  pickVideoModel,
  segmentParams,
  videoModelSupport,
} from "@/utils/drama/video-models";

const caps = (patch: Partial<Capabilities> = {}): Capabilities => ({
  ops: ["t2v", "i2v", "omni"],
  refs: {
    image: { on: true, max: 9, max_mb: 10 },
    audio: { on: true, max: 3, max_mb: 10 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 2000 },
  params: {
    duration: {
      type: "number",
      label: "时长",
      open: true,
      min: 4,
      max: 15,
      default: 5,
      unit: "秒",
    },
    resolution: {
      type: "enum",
      label: "分辨率",
      open: true,
      options: ["720P", "1080P"],
      default: "720P",
    },
    aspect_ratio: {
      type: "enum",
      label: "比例",
      open: true,
      options: ["Auto", "16:9", "9:16"],
      default: "Auto",
    },
  },
  ...patch,
});

const model = (key: string, c: Capabilities): ModelInfo => ({
  key,
  kind: "video",
  label: key,
  pricing: { billing: "per_second", per_second: 2 },
  capabilities: c,
});

describe("视频模型是否适合短剧片段：要能收参考图", () => {
  test("收参考图且支持 omni 或 i2v：可用", () => {
    expect(videoModelSupport(model("a", caps())).ok).toBe(true);
    expect(videoModelSupport(model("b", caps({ ops: ["i2v"] }))).ok).toBe(true);
  });

  test("不收参考图：置灰并写原因（原型 vmodel）", () => {
    const off = caps({
      refs: {
        image: { on: false, max: 0, max_mb: 0 },
        audio: { on: false, max: 0, max_mb: 0 },
        video: { on: false, max: 0, max_mb: 0 },
      },
    });
    const support = videoModelSupport(model("c", off));
    expect(support.ok).toBe(false);
    expect(support.reason).toContain("不支持参考图");
  });

  test("只有文生视频：不可用，原因说明没有图生 / 参考方式", () => {
    const support = videoModelSupport(model("d", caps({ ops: ["t2v"] })));
    expect(support.ok).toBe(false);
    expect(support.reason).toBeTruthy();
  });
});

describe("时长上限与适配", () => {
  test("数字参数取 max；枚举取最大的数字选项；没有时长参数为 null", () => {
    expect(modelMaxSeconds(caps())).toBe(15);
    const enumCaps = caps({
      params: {
        duration: { type: "enum", label: "时长", open: true, options: ["5", "10", 8], default: 5 },
      },
    });
    expect(modelMaxSeconds(enumCaps)).toBe(10);
    expect(modelMaxSeconds(caps({ params: {} }))).toBeNull();
  });

  test("fitDuration：数字夹在范围内，枚举取不小于它的最近选项（都比它小取最大）", () => {
    const num = caps().params!.duration as ParamField;
    expect(fitDuration(num, 12)).toEqual({ value: 12, adjusted: false });
    expect(fitDuration(num, 30)).toEqual({ value: 15, adjusted: true });
    expect(fitDuration(num, 2)).toEqual({ value: 4, adjusted: true });
    const en: ParamField = { type: "enum", label: "时长", open: true, options: [5, 10] };
    expect(fitDuration(en, 7)).toEqual({ value: 10, adjusted: true });
    expect(fitDuration(en, 5)).toEqual({ value: 5, adjusted: false });
    expect(fitDuration(en, 12)).toEqual({ value: 10, adjusted: true });
  });

  test("摘要：参考图上限、参考音频、时长上限", () => {
    expect(modelSummary(model("a", caps()))).toBe("参考图 ≤9 · 参考音频 · ≤15s");
    const noAudio = caps({
      refs: { ...caps().refs, audio: { on: false, max: 0, max_mb: 0 } },
    });
    expect(modelSummary(model("a", noAudio))).toBe("参考图 ≤9 · ≤15s");
  });
});

describe("提交参数：时长跟片段、比例跟短剧", () => {
  test("duration 取片段时长（按模型适配），比例选项里有短剧比例就用，分辨率用用户选的", () => {
    const params = segmentParams(caps(), {
      seconds: 12,
      ratio: "9:16",
      overrides: { resolution: "1080P" },
    });
    expect(params).toMatchObject({ duration: 12, aspect_ratio: "9:16", resolution: "1080P" });
  });

  test("比例选项里没有短剧比例：不覆盖，按模型默认；没有的参数不乱加", () => {
    const params = segmentParams(caps(), { seconds: 20, ratio: "21:9", overrides: {} });
    expect(params.duration).toBe(15);
    expect(params.aspect_ratio).toBeUndefined();
    const bare = segmentParams(caps({ params: {} }), { seconds: 8, ratio: "9:16", overrides: {} });
    expect(bare).toEqual({});
  });
});

describe("默认模型", () => {
  const list = [
    model("nope", caps({ ops: ["t2v"] })),
    model("first", caps()),
    model("second", caps()),
  ];

  test("没存过选择：取第一个可用的；存过且仍可用：沿用；存的已不可用：回到第一个可用", () => {
    expect(pickVideoModel(list, "")?.key).toBe("first");
    expect(pickVideoModel(list, "second")?.key).toBe("second");
    expect(pickVideoModel(list, "nope")?.key).toBe("first");
    expect(pickVideoModel(list, "gone")?.key).toBe("first");
    expect(pickVideoModel([list[0]], "")).toBeUndefined();
  });
});
