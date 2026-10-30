import { afterEach, describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import { useModelsStore } from "@/store/models";
import { canLinkNodes, opForLink } from "@/utils/canvas/link-rule";

const character = { kind: "character" as const };

const refs = (image: boolean, audio: boolean): Capabilities["refs"] => ({
  image: { on: image, max: 4, max_mb: 10 },
  audio: { on: audio, max: 1, max_mb: 10 },
  video: { on: false, max: 0, max_mb: 0 },
});

const setModels = (video: Capabilities, image?: Capabilities) =>
  useModelsStore.setState({
    byKind: {
      video: { status: "ready", models: [{ key: "v1", capabilities: video }] },
      ...(image
        ? { image: { status: "ready", models: [{ key: "i1", capabilities: image }] } }
        : {}),
    } as never,
  });

const omniVideo = (): Capabilities => ({
  ops: ["t2v", "i2v", "omni"],
  refs: refs(true, true),
  prompt: { max_length: 100 },
  params: {},
});
const imageOnlyVideo = (): Capabilities => ({
  ops: ["i2v"],
  refs: refs(true, false),
  prompt: { max_length: 100 },
  params: {},
});
const textOnlyVideo = (): Capabilities => ({
  ops: ["t2v"],
  refs: refs(false, false),
  prompt: { max_length: 100 },
  params: {},
});
const imageModel = (): Capabilities => ({
  ops: ["t2i", "i2i"],
  refs: refs(true, false),
  prompt: { max_length: 100 },
  params: {},
});

afterEach(() => useModelsStore.setState({ byKind: {} }));

describe("角色卡的连线规则（设计 §8.2 第 5 条）", () => {
  test("只能连图片、视频生成节点：音频、文本、导演台、角色卡都不行", () => {
    setModels(omniVideo(), imageModel());
    expect(canLinkNodes(character, { kind: "video", model: "v1" })).toBe(true);
    expect(canLinkNodes(character, { kind: "image", model: "i1" })).toBe(true);
    expect(canLinkNodes(character, { kind: "audio" })).toBe(false);
    expect(canLinkNodes(character, { kind: "script" })).toBe(false);
    expect(canLinkNodes(character, { kind: "director" })).toBe(false);
    expect(canLinkNodes(character, { kind: "character" })).toBe(false);
  });

  test("文生视频节点也能接：换一种方式就收图片或音频即放行，连上后切到图片和音频都收的全能参考", () => {
    setModels(omniVideo());
    const t2v = { kind: "video" as const, model: "v1", params: { op: "t2v" } };
    expect(canLinkNodes(character, t2v)).toBe(true);
    expect(opForLink(character, t2v)).toBe("omni");
    expect(opForLink(character, { ...t2v, params: { op: "omni" } })).toBeUndefined();
  });

  test("只收图片的方式也放行（声音由绑定阶段丢弃），模型什么参考都不收则拒绝", () => {
    setModels(imageOnlyVideo());
    const target = { kind: "video" as const, model: "v1" };
    expect(canLinkNodes(character, target)).toBe(true);
    setModels(textOnlyVideo());
    expect(canLinkNodes(character, target)).toBe(false);
  });

  test("模型清单还没到：不拦，按种类规则放行", () => {
    expect(canLinkNodes(character, { kind: "video" })).toBe(true);
  });
});
