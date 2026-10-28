import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, FlowNode } from "@/types";
import {
  assetEpisodesOf,
  assetStatusOf,
  assetTypeOf,
  countByType,
  defaultDramaForNew,
  dramaForNewAsset,
  episodesLabel,
  filterAssets,
  matchesFilter,
  newNodeKindOf,
  pendingGenerate,
  setEpisodes,
  toAssetItems,
  toggleEpisode,
  type AssetItem,
} from "@/utils/drama/assets";

const node = (id: string, data: Partial<CanvasNodeData>): FlowNode =>
  ({
    id,
    type: "canvas",
    position: { x: 0, y: 0 },
    data: { kind: "image", label: id, ...data },
  }) as FlowNode;

const item = (
  id: string,
  type: AssetItem["type"],
  episodes: number[] | null,
  status: AssetItem["status"] = "idle",
): AssetItem => ({
  id,
  type,
  name: id,
  look: "",
  label: id,
  episodes,
  status,
  data: { kind: "image", label: id },
});

describe("资产分类：节点 -> 类型", () => {
  test("drama.type 优先，其次按节点种类", () => {
    expect(
      assetTypeOf({ kind: "character", label: "a", drama: { type: "character", episodes: [] } }),
    ).toBe("character");
    expect(
      assetTypeOf({ kind: "image", label: "a", drama: { type: "scene", episodes: [1] } }),
    ).toBe("scene");
    expect(assetTypeOf({ kind: "image", label: "a", drama: { type: "prop", episodes: [] } })).toBe(
      "prop",
    );
    expect(assetTypeOf({ kind: "image", label: "a" })).toBe("image");
    expect(assetTypeOf({ kind: "script", label: "a" })).toBe("text");
    expect(assetTypeOf({ kind: "video", label: "a" })).toBe("video");
    expect(assetTypeOf({ kind: "audio", label: "a" })).toBe("audio");
    expect(assetTypeOf({ kind: "character", label: "a" })).toBe("character");
  });

  test("导演台不算资产", () => {
    expect(assetTypeOf({ kind: "director", label: "d" })).toBeNull();
  });

  test("没有 drama 字段 = 未分类（null）；空数组 = 全集", () => {
    expect(assetEpisodesOf({ kind: "image", label: "a" })).toBeNull();
    expect(assetEpisodesOf({ kind: "image", label: "a", drama: { episodes: [] } })).toEqual([]);
    expect(assetEpisodesOf({ kind: "image", label: "a", drama: { episodes: [3, 1, 3] } })).toEqual([
      1, 3,
    ]);
  });

  test("toAssetItems：跳过组与导演台，名字取 drama.name + look", () => {
    const items = toAssetItems([
      { id: "g", type: "group", position: { x: 0, y: 0 }, data: { label: "组" } } as FlowNode,
      node("d", { kind: "director" }),
      node("a", {
        kind: "character",
        label: "林川 · 童年",
        drama: { type: "character", name: "林川", look: "童年", episodes: [1, 2] },
      }),
      node("b", { label: "随手建的" }),
    ]);
    expect(items.map((i) => i.id)).toEqual(["a", "b"]);
    expect(items[0]).toMatchObject({
      name: "林川",
      look: "童年",
      type: "character",
      episodes: [1, 2],
    });
    expect(items[1]).toMatchObject({ name: "随手建的", look: "", episodes: null });
  });
});

describe("集数筛选与「全集 / 未分类」语义", () => {
  test("全集筛选：所有已分类的都显示（含只出现在某几集的），未分类不显示", () => {
    expect(matchesFilter([], { kind: "all" })).toBe(true);
    expect(matchesFilter([2], { kind: "all" })).toBe(true);
    expect(matchesFilter(null, { kind: "all" })).toBe(false);
  });

  test("第 N 集：出现集数为空（全集）或包含 N", () => {
    expect(matchesFilter([], { kind: "episode", no: 3 })).toBe(true);
    expect(matchesFilter([1, 3], { kind: "episode", no: 3 })).toBe(true);
    expect(matchesFilter([1, 2], { kind: "episode", no: 3 })).toBe(false);
    expect(matchesFilter(null, { kind: "episode", no: 3 })).toBe(false);
  });

  test("未分类：只显示没有 drama 字段的", () => {
    expect(matchesFilter(null, { kind: "none" })).toBe(true);
    expect(matchesFilter([], { kind: "none" })).toBe(false);
    expect(matchesFilter([1], { kind: "none" })).toBe(false);
  });

  test("filterAssets 同时按类型和集数", () => {
    const items = [
      item("c1", "character", [1, 2]),
      item("c2", "character", []),
      item("c3", "character", [4]),
      item("s1", "scene", [1]),
      item("x", "character", null),
    ];
    expect(filterAssets(items, "character", { kind: "episode", no: 2 }).map((i) => i.id)).toEqual([
      "c1",
      "c2",
    ]);
    expect(filterAssets(items, "character", { kind: "all" }).map((i) => i.id)).toEqual([
      "c1",
      "c2",
      "c3",
    ]);
    expect(filterAssets(items, "character", { kind: "none" }).map((i) => i.id)).toEqual(["x"]);
  });
});

describe("资产合并后的列表计数", () => {
  test("各类型计数与当前筛选一致，换筛选计数跟着变", () => {
    const items = [
      item("c1", "character", [1]),
      item("c2", "character", [2]),
      item("s1", "scene", []),
      item("p1", "prop", [1, 2]),
      item("i1", "image", null),
    ];
    expect(countByType(items, { kind: "all" })).toMatchObject({
      character: 2,
      scene: 1,
      prop: 1,
      image: 0,
    });
    expect(countByType(items, { kind: "episode", no: 1 })).toMatchObject({
      character: 1,
      scene: 1,
      prop: 1,
    });
    expect(countByType(items, { kind: "none" })).toMatchObject({ image: 1, character: 0 });
  });

  test("后端并入新集号后（同一节点 episodes 变长）计数不重复", () => {
    const before = [item("c1", "character", [1])];
    const after = [item("c1", "character", [1, 2])];
    expect(countByType(before, { kind: "all" }).character).toBe(1);
    expect(countByType(after, { kind: "all" }).character).toBe(1);
    expect(countByType(after, { kind: "episode", no: 2 }).character).toBe(1);
    expect(countByType(before, { kind: "episode", no: 2 }).character).toBe(0);
  });
});

describe("资产状态", () => {
  test("生成中 / 失败优先", () => {
    expect(assetStatusOf({ kind: "image", label: "a", status: "running" })).toBe("running");
    expect(assetStatusOf({ kind: "image", label: "a", status: "error" })).toBe("error");
  });

  test("有素材算已完成，没有算待生成", () => {
    expect(assetStatusOf({ kind: "image", label: "a", src: "https://x/1.png" })).toBe("done");
    expect(assetStatusOf({ kind: "image", label: "a", src: "blob:xx" })).toBe("idle");
    expect(assetStatusOf({ kind: "script", label: "a", text: "正文" })).toBe("done");
    expect(assetStatusOf({ kind: "image", label: "a" })).toBe("idle");
  });

  test("角色卡节点看自己有没有主图（空节点是待生成）", () => {
    const empty: CanvasNodeData = { kind: "character", label: "a" };
    expect(assetStatusOf(empty)).toBe("idle");
    expect(assetStatusOf({ ...empty, src: "https://x/1.png", assetId: "1" })).toBe("done");
    expect(assetStatusOf({ ...empty, src: "blob:xx" })).toBe("idle");
    expect(assetStatusOf({ ...empty, src: "https://x/1.png", status: "running" })).toBe("running");
  });

  test("资产提示词任务还在跑、节点还没有提示词：分析设定中", () => {
    const data: CanvasNodeData = { kind: "image", label: "a", drama: { episodes: [] } };
    expect(assetStatusOf(data, { analyzing: true })).toBe("analyzing");
    expect(assetStatusOf({ ...data, params: { prompt: "夜景" } }, { analyzing: true })).toBe(
      "idle",
    );
  });

  test("可批量生成的：角色 / 场景 / 道具里待生成或失败、已分类的", () => {
    const items = [
      item("a", "character", [1], "idle"),
      item("b", "character", [], "error"),
      item("c", "character", [1], "done"),
      item("d", "character", null, "idle"),
      item("e", "character", [1], "running"),
      item("f", "scene", [1], "idle"),
    ];
    expect(pendingGenerate(items, "character").map((i) => i.id)).toEqual(["a", "b"]);
    expect(pendingGenerate(items, "image")).toEqual([]);
  });
});

describe("出现集数读写", () => {
  test("勾选 / 取消勾选保持升序去重", () => {
    expect(toggleEpisode([], 2)).toEqual([2]);
    expect(toggleEpisode([3], 1)).toEqual([1, 3]);
    expect(toggleEpisode([1, 3], 3)).toEqual([1]);
  });

  test("改集数保留 drama 里的其他字段（别名等）", () => {
    const patch = setEpisodes(
      {
        kind: "image",
        label: "a",
        drama: { type: "scene", name: "村口", episodes: [1], aliases: ["入口"] },
      },
      [1, 2],
    );
    expect(patch.drama).toEqual({
      type: "scene",
      name: "村口",
      episodes: [1, 2],
      aliases: ["入口"],
    });
  });

  test("未分类的节点勾集数，等于归类为全集再设置", () => {
    expect(setEpisodes({ kind: "image", label: "a" }, [2]).drama).toEqual({ episodes: [2] });
  });

  test("新建节点默认全集；角色默认类型 character；已有 drama 不覆盖", () => {
    expect(defaultDramaForNew({ kind: "image", label: "a" })).toEqual({ episodes: [] });
    expect(defaultDramaForNew({ kind: "character", label: "a" })).toEqual({
      type: "character",
      episodes: [],
    });
    const existing = { type: "prop" as const, episodes: [2] };
    expect(defaultDramaForNew({ kind: "image", label: "a", drama: existing })).toBe(existing);
  });
});

describe("展示与新建", () => {
  test("出现集数文案：未分类 / 全集 / 连续集号合并", () => {
    expect(episodesLabel(null)).toBe("未分类");
    expect(episodesLabel([])).toBe("全集");
    expect(episodesLabel([2])).toBe("第 2 集");
    expect(episodesLabel([1, 2, 3, 5])).toBe("第 1–3、5 集");
    expect(episodesLabel([5, 1, 3, 2])).toBe("第 1–3、5 集");
  });

  test("新建节点的种类：角色就是角色卡节点（空节点，和场景、道具一样直接建）", () => {
    expect(newNodeKindOf("character")).toBe("character");
    expect(newNodeKindOf("scene")).toBe("image");
    expect(newNodeKindOf("text")).toBe("script");
  });

  test("在不同筛选下新建：全集默认 / 第 N 集 / 未分类", () => {
    expect(dramaForNewAsset("scene", { kind: "all" })).toEqual({ type: "scene", episodes: [] });
    expect(dramaForNewAsset("prop", { kind: "episode", no: 3 })).toEqual({
      type: "prop",
      episodes: [3],
    });
    expect(dramaForNewAsset("image", { kind: "all" })).toEqual({ episodes: [] });
    expect(dramaForNewAsset("scene", { kind: "none" })).toBeNull();
  });
});
