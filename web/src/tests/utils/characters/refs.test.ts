import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import type { CharacterDto } from "@/api/character/type";
import type { CharacterEntry } from "@/store/characters";
import { applyCharacters, characterRefsOf } from "@/utils/characters/refs";
import type { IncomingLink } from "@/utils/tasks/capabilities";

const asset = (id: string): AssetDto => ({ id, kind: "image" }) as AssetDto;

const card = (patch: Partial<CharacterDto> = {}): CharacterDto => ({
  id: "c1",
  name: "林晚",
  description: "",
  avatar: asset("1"),
  extras: [asset("2")],
  voice: { ...asset("9"), kind: "audio" },
  createdAt: "",
  updatedAt: "",
  ...patch,
});

const ready = (c: CharacterDto): CharacterEntry => ({ status: "ready", card: c, loadedAt: 1 });

describe("characterRefsOf：缓存项 → 绑定要用的素材引用", () => {
  test("读到了：主图、补充图、声音的素材 id", () => {
    expect(characterRefsOf(ready(card()))).toEqual({
      id: "c1",
      name: "林晚",
      avatarAssetId: "1",
      extraAssetIds: ["2"],
      voiceAssetId: "9",
    });
    expect(characterRefsOf(ready(card({ voice: null })))?.voiceAssetId).toBeNull();
  });

  test("已删除（missing）或主图素材已不可用：失效，是 null", () => {
    expect(characterRefsOf({ status: "missing", loadedAt: 1 })).toBeNull();
    expect(characterRefsOf(ready(card({ avatar: null })))).toBeNull();
  });

  test("还在加载、没读过、读取失败且没有旧内容：不下结论，是 undefined", () => {
    expect(characterRefsOf(undefined)).toBeUndefined();
    expect(characterRefsOf({ status: "loading", loadedAt: 0 })).toBeUndefined();
    expect(characterRefsOf({ status: "error", loadedAt: 0 })).toBeUndefined();
  });

  test("读取失败但有旧内容：沿用旧内容（提交前的强制刷新另行拦截）", () => {
    expect(characterRefsOf({ status: "error", card: card(), loadedAt: 0 })?.avatarAssetId).toBe(
      "1",
    );
  });
});

describe("applyCharacters：把缓存里的角色卡写进角色卡连线", () => {
  const base = (patch: Partial<IncomingLink>): IncomingLink => ({
    edgeId: "e",
    sourceId: "n",
    sourceKind: "character",
    sourceLabel: "角色",
    targetHandle: null,
    characterId: "c1",
    ...patch,
  });

  test("只改角色卡连线，别的连线原样", () => {
    const image = base({ sourceKind: "image", characterId: undefined, assetId: "5" });
    const out = applyCharacters([base({}), image], { c1: ready(card()) });
    expect(out[0].character?.avatarAssetId).toBe("1");
    expect(out[1]).toBe(image);
  });

  test("缺 characterId 的旧数据按失效处理", () => {
    expect(applyCharacters([base({ characterId: undefined })], {})[0].character).toBeNull();
  });
});
