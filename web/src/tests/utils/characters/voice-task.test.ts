import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import {
  applyVoiceResult,
  buildVoiceInput,
  VOICE_NODE_PREFIX,
  voiceNodeId,
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
