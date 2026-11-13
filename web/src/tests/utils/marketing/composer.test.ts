import { describe, expect, test } from "bun:test";

import type { ModelInfo } from "@/api/model/type";
import {
  angleLabel,
  buildVideoParams,
  evaluateMarketingSend,
  pickPlatform,
  planCount,
  reconcileVideoParams,
  refVideoProblem,
  toRoundSettings,
  toggleAngle,
  videoModelSupport,
  visionModels,
  type ComposerDraft,
} from "@/utils/marketing/composer";
import { DEFAULT_PLATFORMS } from "@/utils/marketing/options";

const caps = (patch: Partial<ModelInfo["capabilities"]> = {}): ModelInfo["capabilities"] => ({
  ops: ["omni"],
  refs: {
    image: { on: true, max: 9, max_mb: 10 },
    video: { on: true, max: 1, max_mb: 50 },
    audio: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 4000 },
  params: {
    aspect_ratio: {
      type: "enum",
      label: "画幅",
      open: true,
      options: ["16:9", "9:16", "3:4", "1:1"],
      default: "9:16",
    },
    resolution: { type: "enum", label: "分辨率", open: true, options: ["480p", "720p", "1080p"] },
    duration: { type: "enum", label: "时长", open: true, options: [5, 10, 15] },
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

const draft = (patch: Partial<ComposerDraft> = {}): ComposerDraft => ({
  platform: "douyin",
  language: "zh",
  angles: [],
  autoCount: 2,
  adaptCount: 1,
  aspectRatio: "9:16",
  resolution: "720p",
  duration: 15,
  textModel: "",
  description: "",
  ...patch,
});

const baseSend = {
  dock: false,
  productCount: 1,
  hasModelImage: false,
  uploading: false,
  uploadFailed: false,
  ref: null,
  videoModel: model("a"),
  visionModels: [model("t")],
  credits: 11,
  available: 100,
  sending: false,
};

describe("份数与角度", () => {
  test("无参考：份数 = 角度数或自动份数；有参考：跟拍 1 份 + 改编", () => {
    expect(planCount({ hasRef: false, angles: [], autoCount: 3, adaptCount: 1 })).toBe(3);
    expect(
      planCount({ hasRef: false, angles: ["pain", "mood"], autoCount: 3, adaptCount: 1 }),
    ).toBe(2);
    expect(planCount({ hasRef: true, angles: [], autoCount: 3, adaptCount: 2 })).toBe(3);
    expect(planCount({ hasRef: true, angles: ["pain"], autoCount: 3, adaptCount: 2 })).toBe(2);
    expect(planCount({ hasRef: true, angles: [], autoCount: 3, adaptCount: 0 })).toBe(1);
  });

  test("多选角度：无参考最多 4 个，有参考最多改编份数；点已选的取消，取消到空回到自动", () => {
    expect(toggleAngle(["pain"], "mood", 4)).toEqual(["pain", "mood"]);
    expect(toggleAngle(["pain", "mood"], "pain", 4)).toEqual(["mood"]);
    expect(toggleAngle(["pain"], "pain", 4)).toEqual([]);
    expect(toggleAngle(["a", "b"] as never, "mood", 2)).toEqual(["a", "b"]);
  });

  test("发给后端的设置：有参考时改编 0 份清空角度；有参考且选了角度时按改编份数截断", () => {
    const s = (d: Partial<ComposerDraft>, hasRef: boolean) =>
      toRoundSettings({ draft: draft(d), hasRef, videoModel: model("a") });
    expect(s({ adaptCount: 0, angles: ["pain"] }, true)).toMatchObject({
      angles: [],
      autoCount: 0,
    });
    expect(s({ adaptCount: 2, angles: ["pain", "mood", "unbox"] }, true).angles).toEqual([
      "pain",
      "mood",
    ]);
    expect(s({ adaptCount: 2, angles: [] }, true)).toMatchObject({ angles: [], autoCount: 2 });
    expect(s({ autoCount: 3, angles: [] }, false)).toMatchObject({ angles: [], autoCount: 3 });
    const withParams = s({}, false);
    expect(withParams.params).toEqual({ aspect_ratio: "9:16", resolution: "720p", duration: 15 });
    expect(withParams.videoModel).toBe("a");
  });

  test("角度标签：无参考自动 ×N / 单个角度名 / N 个角度；有参考「跟拍 + 改编 ×N」", () => {
    const angles = [
      { key: "pain", name: "痛点-解决", desc: "" },
      { key: "mood", name: "质感大片", desc: "" },
    ] as never;
    expect(angleLabel(draft(), false, angles)).toBe("自动 ×2");
    expect(angleLabel(draft({ angles: ["pain"] }), false, angles)).toBe("痛点-解决");
    expect(angleLabel(draft({ angles: ["pain", "mood"] }), false, angles)).toBe("2 个角度");
    expect(angleLabel(draft({ adaptCount: 2 }), true, angles)).toBe("跟拍 + 改编 ×2");
  });
});

describe("平台切换", () => {
  const platform = (key: string) => DEFAULT_PLATFORMS.find((p) => p.key === key)!;

  test("带出画幅和旁白语言；模型不支持该画幅时退回 9:16", () => {
    const next = pickPlatform(platform("tiktok"), caps());
    expect(next).toEqual({ platform: "tiktok", language: "en", aspectRatio: "9:16" });
    expect(pickPlatform(platform("tmall"), caps())).toMatchObject({
      language: "zh",
      aspectRatio: "3:4",
    });
    const narrow = caps({
      params: {
        aspect_ratio: { type: "enum", label: "画幅", open: true, options: ["16:9", "9:16"] },
      },
    });
    expect(pickPlatform(platform("tmall"), narrow).aspectRatio).toBe("9:16");
    // 平台默认画幅模型支持时照用（亚马逊已下线，这里直接构造一个 16:9 的平台）
    expect(pickPlatform({ ...platform("generic"), aspectRatio: "16:9" }, narrow).aspectRatio).toBe(
      "16:9",
    );
  });
});

describe("视频模型与参数", () => {
  test("没有全能参考的模型禁用并写原因；图片参考上限不足也禁用", () => {
    expect(videoModelSupport(model("a"), 2)).toEqual({ ok: true, reason: null });
    const noOmni = videoModelSupport(model("b", { ops: ["i2v"] }), 1);
    expect(noOmni.ok).toBe(false);
    expect(noOmni.reason).toContain("全能参考");
    const few = videoModelSupport(
      model("c", {
        refs: {
          image: { on: true, max: 2, max_mb: 10 },
          video: { on: false, max: 0, max_mb: 0 },
          audio: { on: false, max: 0, max_mb: 0 },
        },
      }),
      3,
    );
    expect(few.ok).toBe(false);
    expect(few.reason).toContain("2");
  });

  test("参考视频：模型不收、超大、超时长都给原因", () => {
    const label = "模型 a";
    expect(refVideoProblem(caps(), { durationSec: 10, sizeBytes: 1024 }, label)).toBeNull();
    const noVideo = caps({
      refs: {
        image: { on: true, max: 9, max_mb: 10 },
        video: { on: false, max: 0, max_mb: 0 },
        audio: { on: false, max: 0, max_mb: 0 },
      },
    });
    expect(refVideoProblem(noVideo, { durationSec: 10 }, label)).toContain("不支持参考视频");
    expect(refVideoProblem(caps(), { sizeBytes: 60 * 1024 * 1024 }, label)).toContain("50 MB");
    expect(refVideoProblem(caps(), { durationSec: 20 }, label, 15)).toContain("15 秒");
  });

  test("换模型后：当前值仍有效就保留，无效用建议值，再退回 9:16 / 720p / ≤15 秒", () => {
    const next = reconcileVideoParams(
      caps({
        params: {
          aspect_ratio: { type: "enum", label: "画幅", open: true, options: ["16:9", "9:16"] },
          resolution: { type: "enum", label: "分辨率", open: true, options: ["720p", "1080p"] },
          duration: { type: "enum", label: "时长", open: true, options: [6, 10, 20] },
        },
      }),
      { aspectRatio: "3:4", resolution: "480p", duration: 15 },
      "16:9",
    );
    expect(next).toEqual({ aspectRatio: "16:9", resolution: "720p", duration: 10 });
    const keep = reconcileVideoParams(caps(), {
      aspectRatio: "3:4",
      resolution: "1080p",
      duration: 10,
    });
    expect(keep).toEqual({ aspectRatio: "3:4", resolution: "1080p", duration: 10 });
  });

  test("发给后端的参数键名取模型的参数名；模型没有的参数不发", () => {
    expect(
      buildVideoParams(caps(), { aspectRatio: "16:9", resolution: "720p", duration: 10 }),
    ).toEqual({ aspect_ratio: "16:9", resolution: "720p", duration: 10 });
    const ratioOnly = caps({
      params: { ratio: { type: "enum", label: "比例", open: true, options: ["9:16"] } },
    });
    expect(
      buildVideoParams(ratioOnly, { aspectRatio: "9:16", resolution: "720p", duration: 10 }),
    ).toEqual({ ratio: "9:16" });
  });

  test("文本模型只列能看图的", () => {
    const withVision = model("v", { vision: true });
    const without = model("n", { vision: false });
    expect(visionModels([withVision, without]).map((m) => m.key)).toEqual(["v"]);
  });
});

describe("发送禁用原因（按优先级）", () => {
  const args = (patch: Record<string, unknown> = {}) => ({ ...baseSend, ...patch }) as never;
  const ev = (patch: Record<string, unknown> = {}, d: Partial<ComposerDraft> = {}) =>
    evaluateMarketingSend(args(patch), draft(d));

  test("全部满足可以发送", () => {
    expect(ev()).toEqual({ canSend: true, hint: null, tone: null });
  });

  test("没有商品图只禁用、不提示；发送中同理；dock 不要求商品图", () => {
    expect(ev({ productCount: 0 })).toEqual({ canSend: false, hint: null, tone: null });
    expect(ev({ sending: true })).toEqual({ canSend: false, hint: null, tone: null });
    expect(ev({ dock: true, productCount: 0 }).canSend).toBe(true);
  });

  test("没有视频模型只禁用、不提示（模型按钮自己写原因）", () => {
    expect(ev({ videoModel: undefined })).toEqual({ canSend: false, hint: null, tone: null });
  });

  test("所选文本模型不能看图 / 没有能看图的模型", () => {
    const noVision = ev({ visionModels: [] });
    expect(noVision.canSend).toBe(false);
    expect(noVision.hint).toContain("看图");
    const picked = ev({ visionModels: [model("t")] }, { textModel: "other" });
    expect(picked.canSend).toBe(false);
    expect(picked.hint).toContain("不能看图");
    expect(ev({ visionModels: [model("t")] }, { textModel: "t" }).canSend).toBe(true);
  });

  test("优先级：文本模型 > 上传中 > 上传失败 > 参考视频 > 图片上限 > 余额", () => {
    const all = {
      visionModels: [],
      uploading: true,
      uploadFailed: true,
      ref: { sizeBytes: 500 * 1024 * 1024 },
      productCount: 12,
      credits: 500,
    };
    expect(ev(all).hint).toContain("看图");
    expect(ev({ ...all, visionModels: [model("t")] }).hint).toBe("素材还在上传");
    expect(ev({ ...all, visionModels: [model("t")], uploading: false }).hint).toContain("上传失败");
    expect(
      ev({ ...all, visionModels: [model("t")], uploading: false, uploadFailed: false }).hint,
    ).toContain("参考视频");
    expect(
      ev({
        ...all,
        visionModels: [model("t")],
        uploading: false,
        uploadFailed: false,
        ref: null,
      }).hint,
    ).toContain("9");
    expect(
      ev({
        ...all,
        visionModels: [model("t")],
        uploading: false,
        uploadFailed: false,
        ref: null,
        productCount: 1,
      }).hint,
    ).toContain("余额不足");
  });

  test("余额不足写还差多少；没有预估或没拿到余额时不拦（交后端兜底）", () => {
    const low = ev({ credits: 30, available: 12 });
    expect(low).toMatchObject({ canSend: false, tone: "bad" });
    expect(low.hint).toBe("余额不足，还差 18 积分");
    expect(ev({ credits: null, available: 3 }).canSend).toBe(true);
    expect(ev({ credits: 30, available: null }).canSend).toBe(true);
  });

  test("上传中是警告语气，其余是错误语气", () => {
    expect(ev({ uploading: true }).tone).toBe("warn");
    expect(ev({ uploadFailed: true }).tone).toBe("bad");
  });
});
