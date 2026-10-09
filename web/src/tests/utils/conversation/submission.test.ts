import { describe, expect, test } from "bun:test";

import type { ModelInfo, Pricing } from "@/api/model/type";
import { defaultCapabilities, defaultPricing } from "@/utils/admin/model-template";
import { evaluateSend, refIdsByKind, type ComposerRef } from "@/utils/conversation/submission";

const model = (kind: "image" | "video", pricing?: Pricing): ModelInfo => ({
  key: `${kind}-1`,
  kind,
  label: `${kind} 模型`,
  pricing: pricing ?? { billing: "per_call", unit: 3 },
  capabilities: defaultCapabilities(kind),
});

const done = (assetId: string, kind: ComposerRef["kind"] = "image"): ComposerRef => ({
  id: `ref-${kind}-${assetId}`,
  kind,
  assetId,
  name: `${assetId}.${kind}`,
  status: "done",
});

const base = {
  model: model("image"),
  text: "雨夜的城市",
  params: {} as Record<string, unknown>,
  refs: [] as ComposerRef[],
  available: 100 as number | null,
  sending: false,
};

describe("evaluateSend：能不能发、为什么、花多少积分", () => {
  test("默认参数：按次计价，数量 1，input 带提示词和开放参数的默认值", () => {
    const state = evaluateSend(base);
    expect(state.canSend).toBe(true);
    expect(state.hint).toBeNull();
    expect(state.count).toBe(1);
    expect(state.creditsEach).toBe(3);
    expect(state.creditsTotal).toBe(3);
    expect(state.input.prompt).toBe("雨夜的城市");
    expect(state.input.aspect_ratio).toBe("1:1");
  });

  test("生成数量是模型参数：数量 × 单价，count 与 input 里的数量一致", () => {
    const state = evaluateSend({ ...base, params: { count: 4 } });
    expect(state.count).toBe(4);
    expect(state.creditsTotal).toBe(12);
    expect(state.input.count).toBe(4);
  });

  test("按秒计费的视频：单价随时长变化", () => {
    const video = model("video", defaultPricing("video"));
    const short = evaluateSend({ ...base, model: video, params: { duration: 5 } });
    const long = evaluateSend({ ...base, model: video, params: { duration: 10 } });
    expect(short.canSend && long.canSend).toBe(true);
    expect(long.creditsEach).toBe(short.creditsEach * 2);
  });

  test("参考图的 asset id 以数字写进 input.images", () => {
    const state = evaluateSend({ ...base, refs: [done("11"), done("12")] });
    expect(state.canSend).toBe(true);
    expect(state.input.images).toEqual([11, 12]);
  });

  test("没写提示词：不能发，也不提示（刚打开页面不该一片红）", () => {
    for (const text of ["", "   "]) {
      const state = evaluateSend({ ...base, text });
      expect(state.canSend).toBe(false);
      expect(state.hint).toBeNull();
    }
  });

  test("正在发送：不能再发，也不提示", () => {
    const state = evaluateSend({ ...base, sending: true });
    expect(state.canSend).toBe(false);
    expect(state.hint).toBeNull();
  });

  test("余额不足：禁止发送并写明需要和可用", () => {
    const state = evaluateSend({ ...base, params: { count: 4 }, available: 5 });
    expect(state.canSend).toBe(false);
    expect(state.hint).toBe("余额不足（需 12，可用 5）");
    expect(state.tone).toBe("bad");
  });

  test("余额还没拿到：不拦截，由后端兜底", () => {
    expect(evaluateSend({ ...base, available: null }).canSend).toBe(true);
  });

  test("没有可用模型（清单没回来、加载失败、没配置）：不能发，也不提示（模型按钮自己会写明）", () => {
    const state = evaluateSend({ ...base, model: undefined });
    expect(state.canSend).toBe(false);
    expect(state.hint).toBeNull();
  });

  test("参考图：上传中要等，上传失败要处理，超出上限要移除", () => {
    const uploading: ComposerRef = { id: "u", kind: "image", name: "a.png", status: "uploading" };
    const failed: ComposerRef = { id: "f", kind: "image", name: "b.png", status: "error" };
    expect(evaluateSend({ ...base, refs: [uploading] }).hint).toBe("等待参考上传完成");
    expect(evaluateSend({ ...base, refs: [failed] }).hint).toBe(
      "有参考上传失败，重试或移除后再发送",
    );
    const many = ["1", "2", "3", "4", "5"].map((id) => done(id));
    const over = evaluateSend({ ...base, refs: many });
    expect(over.canSend).toBe(false);
    expect(over.hint).toContain("最多 4 个");
  });

  test("模型不接收参考图：已加的参考图不能被悄悄丢掉，要提示并禁止发送", () => {
    const noRefs = model("image");
    noRefs.capabilities = {
      ...noRefs.capabilities,
      ops: ["t2i"],
      refs: {
        image: { on: false, max: 0, max_mb: 0 },
        audio: { on: false, max: 0, max_mb: 0 },
        video: { on: false, max: 0, max_mb: 0 },
      },
    };
    const state = evaluateSend({ ...base, model: noRefs, refs: [done("11")] });
    expect(state.canSend).toBe(false);
    expect(state.hint).toBe("当前模型不支持图片参考，请移除或换个模型");
    // 没有参考图时照常发送
    expect(evaluateSend({ ...base, model: noRefs }).canSend).toBe(true);
  });

  test("提示词超过模型上限：给出模型自己的说法", () => {
    const state = evaluateSend({ ...base, text: "字".repeat(1600) });
    expect(state.canSend).toBe(false);
    expect(state.hint).toContain("1500");
  });
});

describe("视频、音频参考（与画布同一套规则：生成方式由用户设，加素材时自动切到收它的方式）", () => {
  const video = model("video", defaultPricing("video"));
  const omni = { op: "omni" };

  test("各种类的素材 id 分别写进 images / videos / audios", () => {
    const state = evaluateSend({
      ...base,
      model: video,
      params: omni,
      refs: [done("1"), done("2", "video"), done("3", "audio")],
    });
    expect(state.canSend).toBe(true);
    expect(state.input).toMatchObject({ op: "omni", images: [1], videos: [2], audios: [3] });
  });

  test("按种类分别限制数量：视频最多 1 个", () => {
    const state = evaluateSend({
      ...base,
      model: video,
      params: omni,
      refs: [done("2", "video"), done("3", "video")],
    });
    expect(state.canSend).toBe(false);
    expect(state.hint).toContain("最多 1 个");
  });

  test("当前生成方式不收这种素材：提示切换生成方式，不悄悄丢", () => {
    // 默认是文生视频，不收视频参考；模型本身是支持的（全能参考收）
    const state = evaluateSend({ ...base, model: video, refs: [done("2", "video")] });
    expect(state.canSend).toBe(false);
    expect(state.hint).toBe("「文生视频」不接收视频参考，请切换生成方式或移除");
  });

  test("模型根本不收这种素材：提示换模型", () => {
    const noVideo = model("video", defaultPricing("video"));
    noVideo.capabilities = {
      ...noVideo.capabilities,
      refs: { ...noVideo.capabilities.refs, video: { on: false, max: 0, max_mb: 0 } },
    };
    const state = evaluateSend({
      ...base,
      model: noVideo,
      params: omni,
      refs: [done("2", "video")],
    });
    expect(state.canSend).toBe(false);
    expect(state.hint).toBe("当前模型不支持视频参考，请移除或换个模型");
  });
});

describe("refIdsByKind", () => {
  test("按种类分组，只取上传完成且有素材 ID 的", () => {
    const uploading: ComposerRef = { id: "u", kind: "video", name: "a.mp4", status: "uploading" };
    expect(refIdsByKind([done("1"), done("2", "video"), done("3"), uploading])).toEqual({
      image: [1, 3],
      video: [2],
      audio: [],
    });
  });
});
