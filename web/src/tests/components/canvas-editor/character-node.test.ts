import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeCharacter } from "@/types";
import { characterNodeView, voiceCell } from "@/utils/characters/node-view";

const character = (patch: Partial<NodeCharacter> = {}): NodeCharacter => ({
  name: "林晚",
  look: "基础形象",
  description: "",
  extras: [],
  voice: null,
  ...patch,
});
const data = (patch: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
  kind: "character",
  label: "林晚",
  character: character(),
  ...patch,
});
const audio = { mode: "audio" as const, assetId: "20", src: "/a.mp3", origin: "upload" as const };

describe("characterNodeView：角色卡节点的封面状态", () => {
  test("没有主图、没有任务：空节点，标「待补充」，封面给上传 / 素材库入口", () => {
    expect(characterNodeView(data())).toEqual({ phase: "empty", tag: "待补充" });
  });

  test("有主图：就绪", () => {
    expect(characterNodeView(data({ assetId: "10", src: "/a.png" }))).toEqual({
      phase: "ready",
      tag: null,
    });
  });

  test("blob 预览地址不算主图（上传还没落库）", () => {
    expect(characterNodeView(data({ src: "blob:xx" })).phase).toBe("empty");
  });

  test("生成中：不论有没有旧主图都显示生成中", () => {
    expect(characterNodeView(data({ status: "running" }))).toEqual({
      phase: "generating",
      tag: "生成中",
    });
    expect(characterNodeView(data({ status: "running", assetId: "10", src: "/a.png" })).phase).toBe(
      "generating",
    );
  });

  test("生成失败：显示失败和原因，有旧主图时仍是失败（可重试），不吞掉原因", () => {
    expect(characterNodeView(data({ status: "error", error: "内容审核未通过" }))).toEqual({
      phase: "failed",
      tag: "生成失败",
      reason: "内容审核未通过",
    });
    expect(characterNodeView(data({ status: "error" })).reason).toBe("生成失败");
  });

  test("缺角色数据（异常数据）：按空节点处理，不抛错", () => {
    expect(characterNodeView(data({ character: undefined })).phase).toBe("empty");
  });
});

describe("voiceCell：封面下方音色格", () => {
  test("没有音色：空格，点开详情设置", () => {
    expect(voiceCell(character(), false)).toEqual({
      kind: "none",
      title: "还没有音色，点开详情设置",
    });
  });

  test("文本音色：显示文本图标，提示是文本音色（只展示）", () => {
    expect(voiceCell(character({ voice: { mode: "text", text: "年轻女声" } }), false)).toEqual({
      kind: "text",
      title: "文本音色：年轻女声",
    });
  });

  test("音频音色：可试听；目标模型不收参考音频时标「声音未生效」", () => {
    expect(voiceCell(character({ voice: audio }), false)).toEqual({
      kind: "audio",
      title: "音频音色",
      ignored: false,
    });
    expect(voiceCell(character({ voice: audio }), true)).toEqual({
      kind: "audio",
      title: "音频音色（目标模型不收参考音频，声音未生效）",
      ignored: true,
    });
  });

  test("文本音色不会被标成未生效：它本来就不带入", () => {
    expect(voiceCell(character({ voice: { mode: "text", text: "x" } }), true).kind).toBe("text");
  });
});
