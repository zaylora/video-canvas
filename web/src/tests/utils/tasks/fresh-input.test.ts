import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import type { CharacterRefs, IncomingLink } from "@/utils/tasks/capabilities";
import { buildFreshInput } from "@/utils/tasks/fresh-input";

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

/** 角色卡节点此刻的内容：主图 1、补充图 2，音频音色 9（来自节点自带的数据） */
const refs = (patch: Partial<CharacterRefs> = {}): CharacterRefs => ({
  id: "n1",
  name: "林晚",
  avatarAssetId: "1",
  extraAssetIds: ["2"],
  voiceAssetId: "9",
  ...patch,
});

const link = (patch: Partial<IncomingLink> = {}): IncomingLink => ({
  edgeId: "e1",
  sourceId: "n1",
  sourceKind: "character",
  sourceLabel: "林晚",
  targetHandle: null,
  character: refs(),
  ...patch,
});

const params = { op: "omni", prompt: "镜头推近" };

describe("buildFreshInput：按连线里的角色内容组装任务输入", () => {
  test("角色卡节点的主图、补充图当参考图，音频音色当参考音频", () => {
    const out = buildFreshInput(caps(), params, [link()]);
    expect(out.input.images).toEqual([1, 2]);
    expect(out.input.audios).toEqual([9]);
    expect(out.errors).toEqual({});
  });

  test("角色节点还没有形象图（character 为 null）：不带它的素材，也不报错，别的连线照常", () => {
    const out = buildFreshInput(caps(), { ...params }, [
      link({ character: null }),
      link({ edgeId: "e2", sourceKind: "image", character: undefined, assetId: "5" }),
    ]);
    expect(out.input.images).toEqual([5]);
    expect(out.input.audios).toBeUndefined();
  });

  test("目标不收参考音频：声音丢弃，带上提示，其余照常提交", () => {
    const noAudio = caps();
    noAudio.refs.audio = { on: false, max: 0, max_mb: 0 };
    const out = buildFreshInput(noAudio, params, [link()]);
    expect(out.input.images).toEqual([1, 2]);
    expect(out.input.audios).toBeUndefined();
    expect(out.notices).toEqual(["当前模型不支持参考音频，声音未生效"]);
  });

  test("两个形象（同一角色的两个节点）互不串：各自的图按连线顺序依次带入", () => {
    const out = buildFreshInput(caps(), params, [
      link(),
      link({
        edgeId: "e2",
        sourceId: "n2",
        character: refs({ id: "n2", avatarAssetId: "3", extraAssetIds: [], voiceAssetId: null }),
      }),
    ]);
    expect(out.input.images).toEqual([1, 2, 3]);
  });

  test("参考图超出上限：按顺序截断并提示第几张起未带入", () => {
    const small = caps();
    small.refs.image = { on: true, max: 2, max_mb: 10 };
    const out = buildFreshInput(small, params, [
      link(),
      link({ edgeId: "e2", sourceId: "n2", character: refs({ id: "n2", avatarAssetId: "3" }) }),
    ]);
    expect(out.input.images).toEqual([1, 2]);
    expect(out.notices.join("")).toContain("参考图最多 2 张");
  });
});
