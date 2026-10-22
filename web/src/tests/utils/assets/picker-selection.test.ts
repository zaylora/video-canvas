import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import {
  ASSET_PICK_LIMIT,
  addPicked,
  canToggle,
  describeSelection,
  toggleAsset,
} from "@/utils/assets/picker-selection";

const asset = (id: number): AssetDto => ({
  id: String(id),
  kind: "image",
  url: `/files/${id}.png`,
  mimeType: "image/png",
  byteSize: 1,
  width: 1,
  height: 1,
  durationMs: null,
  fileName: `${id}.png`,
});
const many = (n: number) => Array.from({ length: n }, (_, i) => asset(i + 1));

describe("选择上限", () => {
  test("一次最多 20 个", () => {
    expect(ASSET_PICK_LIMIT).toBe(20);
  });
});

describe("toggleAsset：勾选与取消", () => {
  test("没选过就加到末尾，选过就取消", () => {
    const one = toggleAsset([], asset(1));
    expect(one.map((a) => a.id)).toEqual(["1"]);
    const two = toggleAsset(one, asset(2));
    expect(two.map((a) => a.id)).toEqual(["1", "2"]);
    expect(toggleAsset(two, asset(1)).map((a) => a.id)).toEqual(["2"]);
  });

  test("满 20 个后再勾新的不生效，取消已选的仍然可以", () => {
    const full = many(20);
    expect(toggleAsset(full, asset(99))).toBe(full);
    expect(toggleAsset(full, asset(3))).toHaveLength(19);
  });
});

describe("canToggle：卡片是否可点", () => {
  test("没满时都能点", () => {
    expect(canToggle(many(5), asset(99))).toBe(true);
  });

  test("满 20 个：其余卡片禁用，已选的还能点（取消）", () => {
    const full = many(20);
    expect(canToggle(full, asset(99))).toBe(false);
    expect(canToggle(full, asset(7))).toBe(true);
  });
});

describe("addPicked：上传完成后自动勾选", () => {
  test("追加到末尾，已选的不重复", () => {
    const next = addPicked(many(2), [asset(2), asset(3)]);
    expect(next.selected.map((a) => a.id)).toEqual(["1", "2", "3"]);
    expect(next.dropped).toBe(0);
  });

  test("受上限约束：放不下的丢弃并报数量", () => {
    const next = addPicked(many(19), [asset(100), asset(101)]);
    expect(next.selected).toHaveLength(20);
    expect(next.selected.at(-1)?.id).toBe("100");
    expect(next.dropped).toBe(1);
  });
});

describe("describeSelection：底部栏", () => {
  test("0 个：确认禁用并提示「请先选择素材」", () => {
    expect(describeSelection([])).toEqual({
      count: 0,
      label: "已选择 0 个素材",
      confirmDisabled: true,
      confirmHint: "请先选择素材",
      limitHint: null,
    });
  });

  test("有选择：确认可点，没有提示", () => {
    const state = describeSelection(many(3));
    expect(state.label).toBe("已选择 3 个素材");
    expect(state.confirmDisabled).toBe(false);
    expect(state.confirmHint).toBeNull();
    expect(state.limitHint).toBeNull();
  });

  test("满 20 个：提示「一次最多引入 20 个」", () => {
    expect(describeSelection(many(20)).limitHint).toBe("一次最多引入 20 个");
  });
});

describe("自定义上限（角色卡选主图 / 补充图 / 声音）", () => {
  test("单选（limit 1）：点另一个就换成它，点已选的取消，永远能点", () => {
    expect(toggleAsset([asset(1)], asset(2), 1).map((a) => a.id)).toEqual(["2"]);
    expect(toggleAsset([asset(1)], asset(1), 1)).toEqual([]);
    expect(canToggle([asset(1)], asset(2), 1)).toBe(true);
    expect(describeSelection([asset(1)], 1).limitHint).toBeNull();
  });

  test("补充图只剩 2 个名额：选满后其余禁用，提示说明还能选几个", () => {
    expect(canToggle(many(2), asset(9), 2)).toBe(false);
    expect(toggleAsset(many(2), asset(9), 2)).toHaveLength(2);
    expect(describeSelection(many(2), 2).limitHint).toBe("最多再选 2 个");
  });
});
