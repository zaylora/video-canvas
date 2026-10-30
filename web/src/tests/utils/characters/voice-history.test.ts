import { describe, expect, test } from "bun:test";

import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNodeData, NodeCharacter } from "@/types";
import {
  promptExcerpt,
  setVoiceFromHistory,
  voiceHistoryRows,
  withVoiceModel,
} from "@/utils/characters/voice-history";
import { pickVoiceModel } from "@/utils/characters/voice-task";

import { makeTask } from "../tasks/fixtures";

const audio = (assetId: number, over: Partial<TaskView> = {}): TaskView =>
  makeTask({
    kind: "audio",
    node_id: "voice:c1",
    status: "succeeded",
    model_id: "tts-1",
    prompt: "年轻女声，偏低，语速慢",
    created_at: "2026-10-31T10:05:00Z",
    outputs: [{ asset_id: assetId, url: `/files/${assetId}.mp3`, media_type: "audio" }],
    ...over,
  });

const character = (over: Partial<NodeCharacter> = {}): NodeCharacter => ({
  name: "林晚",
  look: "基础形象",
  description: "",
  extras: [],
  voice: null,
  ...over,
});

describe("promptExcerpt：生成记录里的提示词摘要", () => {
  test("短的原样保留，空白折成一个空格", () => {
    expect(promptExcerpt("  年轻女声\n 偏低 ")).toBe("年轻女声 偏低");
  });

  test("超过上限按字符截断并加省略号", () => {
    expect(promptExcerpt("一二三四五六", 4)).toBe("一二三四…");
  });
});

describe("voiceHistoryRows：任务快照变成生成记录的行", () => {
  test("成功的行带音频，和当前声音是同一段素材时标成当前", () => {
    const rows = voiceHistoryRows([audio(7, { id: 2 }), audio(5, { id: 1 })], {
      voiceAssetId: "5",
    });
    expect(rows.map((row) => row.taskId)).toEqual(["2", "1"]);
    expect(rows[0]).toMatchObject({
      prompt: "年轻女声，偏低，语速慢",
      state: { phase: "done", asset: { id: "7", url: "/files/7.mp3" } },
      current: false,
    });
    expect(rows[1].current).toBe(true);
  });

  test("模型名优先用任务快照里的展示名，其次清单里的名字，最后退回模型 key", () => {
    const labels = (key: string) => (key === "tts-2" ? "清单里的名字" : undefined);
    const rows = voiceHistoryRows(
      [
        audio(1, { id: 1, model_label: "快照里的名字" }),
        audio(2, { id: 2, model_id: "tts-2" }),
        audio(3, { id: 3, model_id: "tts-3" }),
      ],
      { modelLabel: labels },
    );
    expect(rows.map((row) => row.model)).toEqual(["快照里的名字", "清单里的名字", "tts-3"]);
  });

  test("失败的行带原因，没有音频的成功按失败，生成中带进度", () => {
    const rows = voiceHistoryRows([
      audio(1, { id: 1, status: "failed", outputs: [], error_message: "内容不合规" }),
      audio(2, { id: 2, outputs: [] }),
      audio(3, { id: 3, status: "running", progress: 40, outputs: null }),
    ]);
    expect(rows[0].state).toEqual({ phase: "failed", reason: "内容不合规" });
    expect(rows[1].state).toEqual({ phase: "failed", reason: "没有生成出音频" });
    expect(rows[2].state).toEqual({ phase: "generating", progress: 40 });
    expect(rows.every((row) => !row.current)).toBe(true);
  });

  test("任务库里有更新的快照时用它的状态，提示词和模型名仍取列表里的", () => {
    const listed = audio(9, {
      id: 4,
      status: "running",
      outputs: null,
      version: 2,
      model_label: "声音模型",
    });
    const live = audio(9, { id: 4, version: 5, prompt: undefined, model_label: undefined });
    const [row] = voiceHistoryRows([listed], { live: (id) => (id === "4" ? live : undefined) });
    expect(row.state).toMatchObject({ phase: "done" });
    expect(row.prompt).toBe("年轻女声，偏低，语速慢");
    expect(row.model).toBe("声音模型");
  });

  test("任务库里的快照更旧时不覆盖列表", () => {
    const listed = audio(9, { id: 4, version: 5 });
    const stale = audio(9, { id: 4, status: "running", outputs: null, version: 2 });
    const [row] = voiceHistoryRows([listed], { live: () => stale });
    expect(row.state.phase).toBe("done");
  });

  test("没有提示词的任务显示空串，时间是短格式", () => {
    const [row] = voiceHistoryRows([audio(1, { prompt: undefined })]);
    expect(row.prompt).toBe("");
    expect(row.createdAt).toMatch(/^\d{2}-\d{2} \d{2}:\d{2}$/);
  });
});

describe("setVoiceFromHistory：把一条生成记录设为音色", () => {
  const data = (over: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
    kind: "character",
    label: "林晚",
    character: character({
      voice: { mode: "audio", assetId: "1", src: "/1.mp3", origin: "upload" },
      voicePrompt: "旧的提示词",
    }),
    ...over,
  });

  test("绑成角色的声音，来源 generated，其余角色数据不变", () => {
    const patch = setVoiceFromHistory(data(), { id: "7", url: "/7.mp3" });
    expect(patch?.character?.voice).toEqual({
      mode: "audio",
      assetId: "7",
      src: "/7.mp3",
      origin: "generated",
    });
    expect(patch?.character?.voicePrompt).toBe("旧的提示词");
  });

  test("已经是当前声音：不用写节点", () => {
    expect(setVoiceFromHistory(data(), { id: "1", url: "/1.mp3" })).toBeNull();
  });

  test("不碰进行中的声音任务记录", () => {
    const patch = setVoiceFromHistory(data({ voiceTaskId: "9" }), { id: "7", url: "/7.mp3" });
    expect(patch).not.toHaveProperty("voiceTaskId");
  });

  test("节点没有角色数据（异常数据）：按节点标题补一个空角色再绑", () => {
    const patch = setVoiceFromHistory(data({ character: undefined }), { id: "7", url: "/7.mp3" });
    expect(patch?.character?.name).toBe("林晚");
    expect(patch?.character?.voice?.assetId).toBe("7");
  });
});

describe("音频模型的选择记在角色数据上", () => {
  const models = [
    { key: "a", label: "A" },
    { key: "b", label: "B" },
  ];

  test("withVoiceModel 写进 character.voiceModel，不动别的字段", () => {
    const data: CanvasNodeData = {
      kind: "character",
      label: "林晚",
      character: character({ voicePrompt: "x" }),
    };
    expect(withVoiceModel(data, "b").character).toEqual(
      character({ voicePrompt: "x", voiceModel: "b" }),
    );
  });

  test("pickVoiceModel 用记着的模型；没记、或记着的已下架，取清单第一个", () => {
    expect(pickVoiceModel(models, "b")?.key).toBe("b");
    expect(pickVoiceModel(models, undefined)?.key).toBe("a");
    expect(pickVoiceModel(models, "gone")?.key).toBe("a");
    expect(pickVoiceModel([], "a")).toBeUndefined();
  });
});
