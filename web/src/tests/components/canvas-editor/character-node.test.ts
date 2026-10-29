import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeCharacter } from "@/types";
import { DEFAULT_NODE_ASPECT } from "@/utils/canvas/node-frame";
import { characterNodeAspect, characterNodeView, voiceCell } from "@/utils/characters/node-view";

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

describe("characterNodeAspect：角色卡节点框跟主图同比例，不再固定 3:4", () => {
  test("主图量到了画幅：用主图的比例（三视图 16:9 就是横框）", () => {
    expect(characterNodeAspect({ measured: 16 / 9, declared: 3 / 4, selected: 1 })).toBeCloseTo(
      16 / 9,
    );
  });

  test("主图还没量到：先用节点记下的主图画幅", () => {
    expect(characterNodeAspect({ measured: null, declared: 9 / 16, selected: 16 / 9 })).toBeCloseTo(
      9 / 16,
    );
  });

  test("还没有主图：跟参数面板选的比例", () => {
    expect(characterNodeAspect({ measured: null, declared: null, selected: 16 / 9 })).toBeCloseTo(
      16 / 9,
    );
  });

  test("什么都没有：和图片节点一样按 16:9", () => {
    expect(characterNodeAspect({ measured: null, declared: null, selected: null })).toBe(
      DEFAULT_NODE_ASPECT,
    );
  });
});

describe("voiceCell：封面下方声音格", () => {
  test("没有声音：空格，点开详情设置", () => {
    expect(voiceCell(character(), false)).toEqual({
      kind: "none",
      title: "还没有声音，点开详情设置",
    });
  });

  test("正在生成声音：显示生成中，哪怕之前绑过音频", () => {
    expect(voiceCell(character(), false, true)).toEqual({
      kind: "generating",
      title: "声音生成中…",
    });
    expect(voiceCell(character({ voice: audio }), false, true).kind).toBe("generating");
  });

  test("有声音：可试听；目标模型不收参考音频时标「声音未生效」", () => {
    expect(voiceCell(character({ voice: audio }), false)).toEqual({
      kind: "audio",
      title: "声音",
      ignored: false,
    });
    expect(voiceCell(character({ voice: audio }), true)).toEqual({
      kind: "audio",
      title: "声音（目标模型不收参考音频，声音未生效）",
      ignored: true,
    });
  });
});
