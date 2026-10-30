import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import {
  applyVoiceResult,
  buildVoiceInput,
  VOICE_NODE_PREFIX,
  voiceNodeId,
  voiceAfterImageStep,
  voiceTaskView,
} from "@/utils/characters/voice-task";
import type { CanvasNodeData } from "@/types";
import { makeTask } from "../tasks/fixtures";

const audioCaps = (over: Partial<Capabilities> = {}): Capabilities => ({
  refs: {
    image: { on: false, max: 0, max_mb: 0 },
    video: { on: false, max: 0, max_mb: 0 },
    audio: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 20 },
  ...over,
});

const done = (over = {}) =>
  makeTask({
    kind: "audio",
    node_id: "voice:n1",
    status: "succeeded",
    outputs: [{ asset_id: 77, url: "/files/v.mp3", media_type: "audio", duration_ms: 8000 }],
    ...over,
  });

describe("voiceNodeId：声音任务的 node_id", () => {
  test("voice:{节点id}，和短剧的 seg: / rec: 一样靠前缀区分，不占画布节点", () => {
    expect(VOICE_NODE_PREFIX).toBe("voice:");
    expect(voiceNodeId("n1")).toBe("voice:n1");
  });
});

describe("voiceTaskView：节点上记着的声音任务此刻是什么状态", () => {
  test("没有记着任务：空闲", () => {
    expect(voiceTaskView(undefined, undefined)).toEqual({ phase: "idle" });
  });

  test("记着任务但本地还没有快照（刚提交 / 刷新后等对账）：算生成中", () => {
    expect(voiceTaskView("5", undefined)).toEqual({ phase: "generating", progress: null });
  });

  test("排队、执行中、落库中都是生成中，带进度", () => {
    for (const status of ["pending", "queued", "running", "finalizing"] as const) {
      expect(voiceTaskView("5", makeTask({ status, progress: 40 }))).toEqual({
        phase: "generating",
        progress: 40,
      });
    }
  });

  test("成功：取第一份音频产出", () => {
    expect(voiceTaskView("5", done())).toEqual({
      phase: "done",
      asset: { id: "77", url: "/files/v.mp3" },
    });
  });

  test("成功但没有音频产出：按失败处理，不凭空绑定", () => {
    expect(voiceTaskView("5", done({ outputs: [] })).phase).toBe("failed");
    expect(
      voiceTaskView("5", done({ outputs: [{ asset_id: 1, url: "/a.png", media_type: "image" }] }))
        .phase,
    ).toBe("failed");
  });

  test("失败、过期：带上原因，没有原因给默认说法", () => {
    expect(
      voiceTaskView("5", makeTask({ status: "failed", error_message: "内容审核未通过" })),
    ).toEqual({ phase: "failed", reason: "内容审核未通过" });
    expect(voiceTaskView("5", makeTask({ status: "expired" }))).toEqual({
      phase: "failed",
      reason: "生成失败",
    });
  });

  test("已取消：单独一种，界面据此清掉记录，不当失败", () => {
    expect(voiceTaskView("5", makeTask({ status: "canceled" }))).toEqual({
      phase: "canceled",
      refunded: true,
    });
  });

  test("已取消且有实扣积分（已进入生成才取消）：标明积分没退", () => {
    expect(voiceTaskView("5", makeTask({ status: "canceled", charged_credits: 2 }))).toEqual({
      phase: "canceled",
      refunded: false,
    });
  });
});

describe("buildVoiceInput：按音频模型能力组装提交内容", () => {
  test("提示词原样提交，去掉首尾空白", () => {
    expect(buildVoiceInput(audioCaps(), "  低沉男声：我回来了  ")).toEqual({
      input: { prompt: "低沉男声：我回来了" },
      error: null,
    });
  });

  test("提示词为空：给出说法，不提交", () => {
    expect(buildVoiceInput(audioCaps(), "   ").error).toBe("请填写提示词");
  });

  test("超出模型的提示词长度上限：给出说法", () => {
    expect(buildVoiceInput(audioCaps(), "啊".repeat(21)).error).toBe("提示词不能超过 20 个字");
  });

  test("模型清单还没到：不能提交", () => {
    expect(buildVoiceInput(undefined, "你好").error).toBe("音频模型还没加载好");
  });

  test("开放给用户、又没有默认值的必填参数：说清楚缺什么（这里没有参数面板）", () => {
    const caps = audioCaps({
      params: { speaker: { type: "enum", label: "发音人", open: true, options: ["a", "b"] } },
    });
    expect(buildVoiceInput(caps, "你好").error).toBe("请填写发音人");
  });

  test("模型收参考图：带上角色主图", () => {
    const caps = audioCaps({
      refs: { ...audioCaps().refs, image: { on: true, max: 1, max_mb: 10 } },
    });
    expect(buildVoiceInput(caps, "你好", "12")).toEqual({
      input: { prompt: "你好", images: [12] },
      error: null,
    });
  });

  test("模型不收参考图：只提交提示词，不因为有主图报错", () => {
    expect(buildVoiceInput(audioCaps(), "你好", "12")).toEqual({
      input: { prompt: "你好" },
      error: null,
    });
  });

  test("收参考图但角色还没有主图（或主图还是本地文件）：只提交提示词", () => {
    const caps = audioCaps({
      refs: { ...audioCaps().refs, image: { on: true, max: 1, max_mb: 10 } },
    });
    expect(buildVoiceInput(caps, "你好").input).toEqual({ prompt: "你好" });
    expect(buildVoiceInput(caps, "你好", "blob:x").input).toEqual({ prompt: "你好" });
  });
});

describe("voiceAfterImageStep：「生成（含音色）」出图后要不要接着生成音色", () => {
  const output = (assetId: string, taskId?: string) => ({
    id: assetId,
    src: `/files/${assetId}.png`,
    mediaType: "image" as const,
    assetId,
    taskId,
    createdAt: 1,
  });
  const data = (over: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
    kind: "character",
    label: "林晚",
    voiceAfterImage: ["7"],
    ...over,
  });

  test("没有记着要生成音色：什么都不做", () => {
    expect(voiceAfterImageStep(data({ voiceAfterImage: undefined }))).toBeNull();
    expect(voiceAfterImageStep(data({ voiceAfterImage: [] }))).toBeNull();
  });

  test("形象还在生成（含一批里还有没结清的）：等着", () => {
    expect(voiceAfterImageStep(data({ status: "running", taskId: "7" }))).toEqual({
      kind: "wait",
    });
    expect(
      voiceAfterImageStep(
        data({ status: "done", voiceAfterImage: ["7", "8"], pendingTaskIds: ["8"] }),
      ),
    ).toEqual({ kind: "wait" });
  });

  test("出图了且当前主图是这次生成的：用新主图生成音色", () => {
    expect(
      voiceAfterImageStep(
        data({
          status: "done",
          assetId: "30",
          outputs: [output("10", "1"), output("30", "7")],
          activeOutputId: "30",
        }),
      ),
    ).toEqual({ kind: "submit", imageAssetId: "30" });
  });

  test("形象失败：放弃音色并说明原因", () => {
    expect(voiceAfterImageStep(data({ status: "error", taskId: "7" }))).toEqual({
      kind: "abort",
      reason: "形象没有生成成功，音色未生成",
    });
  });

  test("形象被取消、节点退回旧主图：不拿旧图生成音色", () => {
    expect(
      voiceAfterImageStep(
        data({ status: "done", assetId: "10", outputs: [output("10", "1")], activeOutputId: "10" }),
      ).kind,
    ).toBe("abort");
    expect(voiceAfterImageStep(data({ status: "idle" })).kind).toBe("abort");
  });
});

describe("applyVoiceResult：任务成功后把音频绑成角色的声音", () => {
  const data = (over: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
    kind: "character",
    label: "林晚",
    voiceTaskId: "5",
    character: {
      name: "林晚",
      look: "基础形象",
      description: "",
      extras: [],
      voice: { mode: "audio", assetId: "1", src: "/old.mp3", origin: "upload" },
    },
    ...over,
  });

  test("换成新音频、来源标成 generated，并清掉任务记录", () => {
    const patch = applyVoiceResult(data(), { id: "77", url: "/files/v.mp3" });
    expect(patch.character?.voice).toEqual({
      mode: "audio",
      assetId: "77",
      src: "/files/v.mp3",
      origin: "generated",
    });
    expect(patch.voiceTaskId).toBeUndefined();
    expect("voiceTaskId" in patch).toBe(true);
  });

  test("节点上已经没有角色数据（异常数据）：只清掉任务记录", () => {
    const patch = applyVoiceResult(data({ character: undefined }), { id: "77", url: "/v.mp3" });
    expect(patch.character).toBeUndefined();
    expect("voiceTaskId" in patch).toBe(true);
  });
});
