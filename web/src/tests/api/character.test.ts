import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import {
  createCharacter,
  deleteCharacter,
  getCharactersByIds,
  listCharacters,
  updateCharacter,
} from "@/api/character";
import type { BackendCharacterDto } from "@/api/character/type";
import service from "@/utils/requests/service";

const rawAsset = (id: number, kind: "image" | "audio" = "image") => ({
  id,
  kind,
  url: `/files/${id}`,
  mime_type: kind === "image" ? "image/png" : "audio/mpeg",
  byte_size: 10,
  width: kind === "image" ? 768 : 0,
  height: kind === "image" ? 1024 : 0,
  duration_ms: kind === "audio" ? 3000 : 0,
  file_name: `f${id}`,
});

const raw = (patch: Partial<BackendCharacterDto> = {}): BackendCharacterDto => ({
  id: "1a2b",
  name: "林晚",
  description: "短发",
  avatar: rawAsset(1),
  extras: [rawAsset(2), rawAsset(3)],
  voice: rawAsset(9, "audio"),
  created_at: "2026-10-22T08:00:00Z",
  updated_at: "2026-10-22T09:00:00Z",
  ...patch,
});

describe("api/character：snake→camel 映射", () => {
  let get: Mock<typeof service.get>;
  let post: Mock<typeof service.post>;
  let patch: Mock<typeof service.patch>;
  let del: Mock<typeof service.delete>;
  beforeEach(() => {
    get = spyOn(service, "get");
    post = spyOn(service, "post");
    patch = spyOn(service, "patch");
    del = spyOn(service, "delete");
  });
  // service 是全仓共用的单例，必须还原
  afterEach(() => {
    get.mockRestore();
    post.mockRestore();
    patch.mockRestore();
    del.mockRestore();
  });

  test("素材走 mapAsset：id 变字符串，0 维度变 null", async () => {
    get.mockResolvedValue({ items: [raw()] });
    const [card] = await listCharacters();
    expect(card.id).toBe("1a2b");
    expect(card.avatar?.id).toBe("1");
    expect(card.extras.map((a) => a.id)).toEqual(["2", "3"]);
    expect(card.voice?.durationMs).toBe(3000);
    expect(card.voice?.width).toBeNull();
    expect(card.createdAt).toBe("2026-10-22T08:00:00Z");
    expect(card.updatedAt).toBe("2026-10-22T09:00:00Z");
  });

  test("主图不可用时 avatar 为 null；声音缺省为 null；extras 的 null 兜底为空数组", async () => {
    get.mockResolvedValue({
      items: [raw({ avatar: null, voice: undefined, extras: null as never })],
    });
    const [card] = await listCharacters();
    expect(card.avatar).toBeNull();
    expect(card.voice).toBeNull();
    expect(card.extras).toEqual([]);
  });

  test("列表：items 为 null 时是空数组；有关键字才带 q", async () => {
    get.mockResolvedValue({ items: null });
    expect(await listCharacters("林")).toEqual([]);
    expect(get.mock.calls[0].slice(0, 2)).toEqual(["/characters", { q: "林" }]);
    await listCharacters();
    expect(get.mock.calls[1][1]).toBeUndefined();
  });

  test("按 ID 批量读：逗号拼接；空数组不发请求", async () => {
    get.mockResolvedValue({ items: [raw()] });
    expect(await getCharactersByIds([])).toEqual([]);
    expect(get).not.toHaveBeenCalled();
    const out = await getCharactersByIds(["1a2b", "3c"]);
    expect(out).toHaveLength(1);
    expect(get.mock.calls[0][0]).toBe("/characters");
    expect(get.mock.calls[0][1]).toEqual({ ids: "1a2b,3c" });
  });

  test("新建：素材 ID 转数字，声音为空时不写 null 以外的值", async () => {
    post.mockResolvedValue(raw());
    await createCharacter({
      name: " 林晚 ",
      description: "x",
      avatarAssetId: "1",
      extraAssetIds: ["2", "3"],
      voiceAssetId: null,
    });
    expect(post.mock.calls[0][0]).toBe("/characters");
    expect(post.mock.calls[0][1]).toEqual({
      name: " 林晚 ",
      description: "x",
      avatar_asset_id: 1,
      extra_asset_ids: [2, 3],
      voice_asset_id: null,
    });
  });

  test("更新：PATCH 全量字段，清空声音写 null、清空补充图写 []", async () => {
    patch.mockResolvedValue(raw({ voice: null, extras: [] }));
    const card = await updateCharacter("1a2b", {
      name: "林晚",
      description: "",
      avatarAssetId: "5",
      extraAssetIds: [],
      voiceAssetId: "9",
    });
    expect(patch.mock.calls[0][0]).toBe("/characters/1a2b");
    expect(patch.mock.calls[0][1]).toMatchObject({
      avatar_asset_id: 5,
      extra_asset_ids: [],
      voice_asset_id: 9,
    });
    expect(card.voice).toBeNull();
  });

  test("删除调用 DELETE /characters/:id", async () => {
    del.mockResolvedValue(undefined);
    await deleteCharacter("1a2b");
    expect(del.mock.calls[0][0]).toBe("/characters/1a2b");
  });
});
