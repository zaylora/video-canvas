import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import {
  deserializeGraph,
  deserializeNode,
  serializeGraph,
} from "@/utils/canvas/canvas-persistence";
import { emptyCharacter } from "@/utils/characters/node-character";

import { makeData } from "../tasks/fixtures";

const viewport = { x: 0, y: 0, zoom: 1 };

const node = (id: string, data: Partial<CanvasNode["data"]>): CanvasNode => ({
  id,
  type: "canvas",
  position: { x: 0, y: 0 },
  data: makeData({ kind: "character", label: id, ...data }),
});

describe("角色卡节点的存取：数据内嵌在画布里", () => {
  test("存档带上 character（含补充图和音频音色），主图沿用 assetId / src", () => {
    const character = {
      ...emptyCharacter("林晚"),
      extras: [{ assetId: "11", src: "/a/11.png" }],
      voice: { mode: "audio" as const, assetId: "20", src: "/a/20.mp3", origin: "upload" as const },
    };
    const graph = serializeGraph(
      [node("c1", { assetId: "10", src: "/a/10.png", mediaType: "image", character })],
      [],
      viewport,
    );
    expect(graph.nodes[0].data).toMatchObject({ kind: "character", assetId: "10", character });
  });

  test("存档带上正在生成的声音任务 id，刷新后还能认领结果；没有就不写", () => {
    const withTask = serializeGraph(
      [node("c1", { character: emptyCharacter("林晚"), voiceTaskId: "9" })],
      [],
      viewport,
    );
    expect(withTask.nodes[0].data).toMatchObject({ voiceTaskId: "9" });
    const without = serializeGraph(
      [node("c1", { character: emptyCharacter("林晚") })],
      [],
      viewport,
    );
    expect(without.nodes[0].data).not.toHaveProperty("voiceTaskId");
  });

  test("存档带上「出图后生成音色」的标记，刷新后形象出图照样接着生成音色；没有就不写", () => {
    const pending = serializeGraph(
      [node("c1", { character: emptyCharacter("林晚"), voiceAfterImage: ["7"] })],
      [],
      viewport,
    );
    expect(pending.nodes[0].data).toMatchObject({ voiceAfterImage: ["7"] });
    const without = serializeGraph(
      [node("c1", { character: emptyCharacter("林晚") })],
      [],
      viewport,
    );
    expect(without.nodes[0].data).not.toHaveProperty("voiceAfterImage");
  });

  test("存档不再写旧字段 characterId / labelCache", () => {
    const graph = serializeGraph(
      [
        node("c1", {
          character: emptyCharacter("林晚"),
          characterId: "old",
          labelCache: { name: "林晚" },
        }),
      ],
      [],
      viewport,
    );
    expect(graph.nodes[0].data).not.toHaveProperty("characterId");
    expect(graph.nodes[0].data).not.toHaveProperty("labelCache");
  });

  test("读档：旧的引用型角色卡节点变成带原名字的空节点，位置不变，不迁移封面", () => {
    const loaded = deserializeNode({
      id: "c1",
      type: "canvas",
      position: { x: 40, y: 50 },
      data: {
        kind: "character",
        label: "小明",
        characterId: "c1",
        labelCache: { name: "小明", coverAssetId: "9" },
      },
    });
    expect(loaded.position).toEqual({ x: 40, y: 50 });
    expect(loaded.data.character).toEqual(emptyCharacter("小明"));
    expect(loaded.data).not.toHaveProperty("characterId");
    expect(loaded.data.assetId).toBeUndefined();
  });

  test("整张画布读档同样规整；存了再读，character 原样回来", () => {
    const saved = serializeGraph(
      [node("c1", { character: { ...emptyCharacter("林晚"), look: "礼服" } })],
      [],
      viewport,
    );
    const back = deserializeGraph(saved);
    expect(back.nodes[0].data.character).toEqual({ ...emptyCharacter("林晚"), look: "礼服" });
    const legacy = deserializeGraph({
      nodes: [
        {
          id: "c9",
          type: "canvas",
          position: { x: 0, y: 0 },
          data: { kind: "character", label: "x", characterId: "z" },
        },
      ],
      edges: [],
      viewport,
    });
    expect(legacy.nodes[0].data.character?.name).toBe("x");
  });
});
