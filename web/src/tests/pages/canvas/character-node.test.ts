import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import type { CharacterDto } from "@/api/character/type";
import { characterNodeView } from "@/pages/canvas/character-node";
import type { CharacterEntry } from "@/store/characters";

const asset = (id: string): AssetDto => ({ id, kind: "image" }) as AssetDto;
const card = (patch: Partial<CharacterDto> = {}): CharacterDto => ({
  id: "c1",
  name: "林晚",
  description: "",
  avatar: asset("1"),
  extras: [],
  voice: null,
  createdAt: "",
  updatedAt: "",
  ...patch,
});
const entry = (patch: Partial<CharacterEntry>): CharacterEntry => ({
  status: "ready",
  loadedAt: 1,
  ...patch,
});

describe("characterNodeView：节点展示状态（设计 §7）", () => {
  test("缓存里还没有、或首次读取中：加载中（骨架）", () => {
    expect(characterNodeView("c1", undefined)).toEqual({ phase: "loading", failed: false });
    expect(characterNodeView("c1", entry({ status: "loading", loadedAt: 0 }))).toEqual({
      phase: "loading",
      failed: false,
    });
  });

  test("读到了：正常，显示最新内容", () => {
    const view = characterNodeView("c1", entry({ card: card() }));
    expect(view).toEqual({ phase: "ready", card: card(), stale: false });
  });

  test("角色卡已删除（查不到）：已失效", () => {
    expect(characterNodeView("c1", entry({ status: "missing" }))).toEqual({
      phase: "dead",
      reason: "原角色卡已被删除",
    });
  });

  test("主图素材已不可用：等同角色卡失效", () => {
    const view = characterNodeView("c1", entry({ card: card({ avatar: null }) }));
    expect(view).toEqual({ phase: "dead", reason: "形象主图已不可用" });
  });

  test("旧数据没有 characterId：已失效，可以重新选择", () => {
    expect(characterNodeView(undefined, undefined).phase).toBe("dead");
  });

  test("读取失败：有旧内容就沿用并标 stale，没有旧内容显示读取失败", () => {
    expect(characterNodeView("c1", entry({ status: "error", card: card() }))).toEqual({
      phase: "ready",
      card: card(),
      stale: true,
    });
    expect(characterNodeView("c1", entry({ status: "error" }))).toEqual({
      phase: "loading",
      failed: true,
    });
  });
});
