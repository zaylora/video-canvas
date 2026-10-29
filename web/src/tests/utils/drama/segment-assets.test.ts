import { describe, expect, test } from "bun:test";

import type { CanvasGraphDto } from "@/api/canvas/type";
import {
  assetsFromGraph,
  countForEpisode,
  countReferences,
  groupByType,
  inEpisode,
  referenceWarning,
  splitByEpisode,
} from "@/utils/drama/segment-assets";

type GraphNode = CanvasGraphDto["nodes"][number];

const node = (id: string, data: Record<string, unknown>): GraphNode =>
  ({
    id,
    type: "canvas",
    position: { x: 0, y: 0 },
    data: { kind: "image", label: id, ...data },
  }) as GraphNode;

const nodes: GraphNode[] = [
  node("c1", {
    kind: "character",
    label: "林川 · 童年",
    character: { name: "林川", look: "童年", description: "", extras: [], voice: null },
    drama: { type: "character", name: "林川", look: "童年", episodes: [1, 2] },
  }),
  node("s1", {
    label: "山村街道",
    src: "https://cdn/s1.png",
    assetId: "11",
    mediaType: "image",
    drama: { type: "scene", name: "山村街道", look: "", episodes: [] },
  }),
  node("p1", {
    label: "玉佩",
    src: "blob:local",
    drama: { type: "prop", name: "玉佩", episodes: [3] },
  }),
  node("loose", { label: "随手建的图", src: "https://cdn/x.png" }),
  node("run", {
    label: "生成中的场景",
    status: "running",
    src: "https://cdn/old.png",
    assetId: "7",
    drama: { type: "scene", name: "井", episodes: [2] },
  }),
  { id: "g", type: "group", position: { x: 0, y: 0 }, width: 1, height: 1, data: { label: "组" } },
  node("dir", { kind: "director", drama: { type: "scene", episodes: [] } }),
];

describe("assetsFromGraph：短剧画布上的资产节点", () => {
  const assets = assetsFromGraph(nodes);

  test("只取有 drama.type 的节点，跳过组、导演台和未分类节点", () => {
    expect(assets.map((a) => a.id)).toEqual(["c1", "s1", "p1", "run"]);
  });

  test("字段映射：造型、节点标题、角色数据；本地 blob 素材不算有图；生成中的标出来", () => {
    expect(assets[0]).toMatchObject({
      type: "character",
      name: "林川",
      look: "童年",
      label: "林川 · 童年",
      character: { name: "林川", look: "童年", description: "", extras: [], voice: null },
      episodes: [1, 2],
    });
    expect(assets[1]).toMatchObject({ src: "https://cdn/s1.png", assetId: "11" });
    expect(assets[2].src).toBeNull();
    expect(assets[3].generating).toBe(true);
  });

  test("旧画布里引用全局角色卡的节点：规整成带原名字的空角色节点，不再带 characterId", () => {
    const legacy = assetsFromGraph([
      node("old", {
        kind: "character",
        label: "老王",
        characterId: "gone",
        labelCache: { name: "老王" },
        drama: { type: "character", name: "老王", look: "", episodes: [] },
      }),
    ]);
    expect(legacy).toHaveLength(1);
    expect(legacy[0].character?.name).toBe("老王");
    expect(legacy[0]).not.toHaveProperty("characterId");
    expect(legacy[0].src).toBeNull();
  });

  test("画布没有节点时是空数组", () => {
    expect(assetsFromGraph(undefined)).toEqual([]);
    expect(assetsFromGraph([])).toEqual([]);
  });
});

describe("按集与分组", () => {
  const assets = assetsFromGraph(nodes);

  test("全集资产算出现在每一集；本集优先", () => {
    expect(inEpisode(assets[1], 5)).toBe(true);
    expect(inEpisode(assets[0], 3)).toBe(false);
    const { here, other } = splitByEpisode(assets, 1);
    expect(here.map((a) => a.id)).toEqual(["c1", "s1"]);
    expect(other.map((a) => a.id)).toEqual(["p1", "run"]);
  });

  test("未分类资产（所在的集被删了，episodes 为 null）不算出现在任何一集", () => {
    const [orphan] = assetsFromGraph([
      {
        id: "o1",
        type: "canvas",
        position: { x: 0, y: 0 },
        data: {
          kind: "image",
          label: "道具",
          drama: { type: "prop", name: "玉佩", episodes: null },
        },
      },
    ] as never);
    expect(orphan.episodes).toBeNull();
    expect(inEpisode(orphan, 1)).toBe(false);
  });

  test("分组按角色 / 场景 / 道具，空组略过", () => {
    expect(groupByType(assets).map((g) => [g.type, g.items.length])).toEqual([
      ["character", 1],
      ["scene", 2],
      ["prop", 1],
    ]);
    expect(groupByType(assets.filter((a) => a.type === "scene")).map((g) => g.type)).toEqual([
      "scene",
    ]);
  });

  test("分集卡片的角色 / 场景计数", () => {
    expect(countForEpisode(assets, 2)).toEqual({ character: 1, scene: 2, prop: 0 });
    expect(countForEpisode(assets, 3)).toEqual({ character: 0, scene: 1, prop: 1 });
  });
});

describe("countReferences：删节点前提示有几个片段引用了它", () => {
  test("按片段计数，同一片段里引用多次只算一次", () => {
    const prompts = [
      "@[林川](c1)在@[山村街道](s1)@[林川](c1)",
      "只有@[山村街道](s1)",
      "没有引用，只有文字 c1",
    ];
    expect(countReferences("c1", prompts)).toBe(1);
    expect(countReferences("s1", prompts)).toBe(2);
    expect(countReferences("zzz", prompts)).toBe(0);
  });
});

describe("referenceWarning：删资产节点前的提示", () => {
  const prompts = ["@[林川](c1)@[玉佩](p1)", "@[山村街道](s1)", "没引用"];

  test("按片段去重计数，列出被引用的资产名", () => {
    expect(
      referenceWarning(
        [
          { id: "c1", label: "林川" },
          { id: "p1", label: "玉佩" },
        ],
        prompts,
      ),
    ).toEqual({ segments: 1, names: ["林川", "玉佩"] });
    expect(referenceWarning([{ id: "s1", label: "山村街道" }], prompts)).toEqual({
      segments: 1,
      names: ["山村街道"],
    });
  });

  test("没有任何片段引用：不提示", () => {
    expect(referenceWarning([{ id: "zzz", label: "x" }], prompts)).toBeNull();
    expect(referenceWarning([{ id: "c1", label: "林川" }], [])).toBeNull();
  });
});
