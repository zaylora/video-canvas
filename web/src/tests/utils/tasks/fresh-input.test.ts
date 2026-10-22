import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import type { Capabilities } from "@/api/model/type";
import type { CharacterDto } from "@/api/character/type";
import type { CharacterEntry } from "@/store/characters";
import type { IncomingLink } from "@/utils/tasks/capabilities";
import { buildFreshInput, characterIdsOf } from "@/utils/tasks/fresh-input";

const asset = (id: string, kind: AssetDto["kind"] = "image"): AssetDto =>
  ({ id, kind }) as AssetDto;
const card = (patch: Partial<CharacterDto> = {}): CharacterDto => ({
  id: "c1",
  name: "林晚",
  description: "",
  avatar: asset("1"),
  extras: [],
  voice: asset("9", "audio"),
  createdAt: "",
  updatedAt: "",
  ...patch,
});
const ready = (c: CharacterDto): CharacterEntry => ({ status: "ready", card: c, loadedAt: 1 });

const caps = (): Capabilities => ({
  ops: ["t2v", "omni"],
  refs: {
    image: { on: true, max: 4, max_mb: 10 },
    audio: { on: true, max: 1, max_mb: 10 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 100 },
  params: {},
});

const link = (patch: Partial<IncomingLink> = {}): IncomingLink => ({
  edgeId: "e1",
  sourceId: "n1",
  sourceKind: "character",
  sourceLabel: "林晚",
  targetHandle: null,
  characterId: "c1",
  ...patch,
});

const params = { op: "omni", prompt: "镜头推近" };

describe("characterIdsOf：提交前要强制刷新的角色卡", () => {
  test("只取角色卡连线，去重，缺 characterId 的旧数据略过", () => {
    const ids = characterIdsOf([
      link(),
      link({ edgeId: "e2", sourceId: "n2" }),
      link({ edgeId: "e3", characterId: "c2" }),
      link({ edgeId: "e4", characterId: undefined }),
      link({ edgeId: "e5", sourceKind: "image", characterId: undefined, assetId: "5" }),
    ]);
    expect(ids).toEqual(["c1", "c2"]);
  });
});

describe("buildFreshInput：用刚读到的角色卡内容组装任务输入", () => {
  test("资产页刚改过：提交用新图和新声音，而不是节点上次渲染时的旧缓存", () => {
    const stale = card({ avatar: asset("1"), voice: null });
    // links 里带着旧内容；最新读到的换了主图、加了声音
    const out = buildFreshInput(caps(), params, [link({ character: undefined })], {
      c1: ready(card({ avatar: asset("7"), extras: [asset("8")], voice: asset("9", "audio") })),
    });
    expect(stale.voice).toBeNull();
    expect(out.input.images).toEqual([7, 8]);
    expect(out.input.audios).toEqual([9]);
    expect(out.errors).toEqual({});
  });

  test("提交前发现卡已被删：不带它的素材（连线保留但不参与）", () => {
    const out = buildFreshInput(
      caps(),
      { ...params },
      [link(), link({ edgeId: "e2", sourceKind: "image", characterId: undefined, assetId: "5" })],
      { c1: { status: "missing", loadedAt: 1 } },
    );
    expect(out.input.images).toEqual([5]);
    expect(out.input.audios).toBeUndefined();
  });

  test("目标不收参考音频：声音丢弃，带上提示，其余照常提交", () => {
    const noAudio = caps();
    noAudio.refs.audio = { on: false, max: 0, max_mb: 0 };
    const out = buildFreshInput(noAudio, params, [link()], { c1: ready(card()) });
    expect(out.input.images).toEqual([1]);
    expect(out.input.audios).toBeUndefined();
    expect(out.notices).toEqual(["当前模型不支持参考音频，声音未生效"]);
  });
});
