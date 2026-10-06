import { describe, expect, test } from "bun:test";

import { useModelsStore } from "@/store/models";
import {
  canLinkNodes,
  mentionableNodes,
  opForLink,
  partitionLinkable,
  unlinkSource,
} from "@/utils/canvas/link-rule";

const node = (id: string, kind: "script" | "image" | "video" | "audio") => ({ id, kind });

describe("partitionLinkable：多选引用时哪些节点能接到新节点上", () => {
  const selected = [
    node("t", "script"),
    node("i", "image"),
    node("v", "video"),
    node("a", "audio"),
  ];

  test("新建图片节点：文本、图片能接，视频、音频接不上", () => {
    const { linkable, skipped } = partitionLinkable(selected, { kind: "image" });
    expect(linkable.map((item) => item.id)).toEqual(["t", "i"]);
    expect(skipped.map((item) => item.id)).toEqual(["v", "a"]);
  });

  test("新建视频节点：四种都能接，视频连视频也行", () => {
    const { linkable, skipped } = partitionLinkable(selected, { kind: "video" });
    expect(linkable.map((item) => item.id)).toEqual(["t", "i", "v", "a"]);
    expect(skipped).toEqual([]);
  });

  test("新建音频节点：只有文本能接", () => {
    const { linkable } = partitionLinkable(selected, { kind: "audio" });
    expect(linkable.map((item) => item.id)).toEqual(["t"]);
  });

  test("保留调用方带的额外字段，方便按 id 去接线", () => {
    const { linkable } = partitionLinkable([{ id: "x", kind: "image" as const, extra: 1 }], {
      kind: "image",
    });
    expect(linkable[0].extra).toBe(1);
  });
});

describe("mentionableNodes：@ 菜单里能列哪些素材", () => {
  const n = (id: string, kind: "script" | "image" | "video" | "audio", extra = {}) => ({
    id,
    data: { kind, label: id, ...extra },
  });
  const img = (id: string) => n(id, "image", { src: `/${id}.png`, assetId: id });
  const nodes = [
    n("target", "video"),
    img("linked"),
    img("free"),
    n("text", "script", { text: "分镜" }),
    n("emptyText", "script", { text: "  " }),
    n("noAsset", "image", { src: "/blob.png" }),
    { ...img("running"), data: { ...img("running").data, status: "running" as const } },
    img("down"),
    n("audioToImage", "audio", { src: "/a.mp3", assetId: "a" }),
  ];
  const edges = [
    { source: "linked", target: "target" },
    { source: "target", target: "down" },
  ];

  test("已连上的进 linked，能接的进 canvas；没素材、生成中、在下游的都不出现", () => {
    const { linked, canvas } = mentionableNodes("target", nodes, edges);
    expect(linked.map((item) => item.id)).toEqual(["linked"]);
    expect(canvas.map((item) => item.id)).toEqual(["free", "text", "audioToImage"]);
  });

  test("种类接不上的不出现：图片节点不收音频、视频；下游的 down 也不收", () => {
    const { canvas } = mentionableNodes("linked", nodes, edges);
    expect(canvas.map((item) => item.id)).toEqual(["free", "text"]);
  });

  test("目标节点不存在时什么也不列", () => {
    expect(mentionableNodes("nope", nodes, edges)).toEqual({ linked: [], canvas: [] });
  });
});

describe("unlinkSource：引用条上点 × 断开某个上游", () => {
  const edges = [
    { id: "a", source: "img", target: "me" },
    { id: "b", source: "img", target: "other" },
    { id: "c", source: "txt", target: "me" },
    { id: "d", source: "img", target: "me" },
  ];

  test("只删这个上游连到本节点的线（重复的也一起删），别的线原样保留", () => {
    expect(unlinkSource(edges, "img", "me").map((edge) => edge.id)).toEqual(["b", "c"]);
  });

  test("没有这根线时原样返回同一个数组，不引起多余的保存和撤销步", () => {
    expect(unlinkSource(edges, "nope", "me")).toBe(edges);
  });
});

describe("视频节点是文生视频时也能接图片：连上后自动切到全能参考", () => {
  const caps = {
    ops: ["t2v", "i2v", "omni"],
    refs: {
      image: { on: true, max: 9, max_mb: 10 },
      audio: { on: true, max: 3, max_mb: 15 },
      video: { on: false, max: 0, max_mb: 0 },
    },
  };
  const setModels = () =>
    useModelsStore.setState({
      byKind: {
        video: { status: "ready", models: [{ key: "v1", capabilities: caps }] },
      } as never,
    });

  test("canLinkNodes：文生视频接图片放行，视频素材关闭仍拒绝", () => {
    setModels();
    const target = { kind: "video" as const, model: "v1", params: { op: "t2v" } };
    expect(canLinkNodes({ kind: "image" }, target)).toBe(true);
    expect(canLinkNodes({ kind: "video" }, target)).toBe(false);
  });

  test("opForLink：需要切时给出全能参考，不需要切时是 undefined", () => {
    setModels();
    const t2v = { kind: "video" as const, model: "v1", params: { op: "t2v" } };
    expect(opForLink({ kind: "image" }, t2v)).toBe("omni");
    expect(opForLink({ kind: "script" }, t2v)).toBeUndefined();
    expect(opForLink({ kind: "image" }, { ...t2v, params: { op: "omni" } })).toBeUndefined();
    expect(opForLink({ kind: "image" }, { kind: "image" })).toBeUndefined();
  });
});
