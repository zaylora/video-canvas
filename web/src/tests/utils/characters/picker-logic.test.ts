import { describe, expect, test } from "bun:test";

import type { CharacterDto } from "@/api/character/type";
import { ASSET_STACK_COLUMNS, ASSET_STACK_GAP } from "@/constants/canvas";
import {
  CHARACTER_NODE_HEIGHT,
  CHARACTER_NODE_WIDTH,
  planCharacterNodes,
} from "@/utils/canvas/character-nodes";
import { NODE_FRAME_SHORT_SIDE, nodeFrameSize } from "@/utils/canvas/node-frame";
import {
  characterPickerSummary,
  filterCharacters,
  toggleCharacter,
} from "@/utils/characters/picker-logic";

const card = (id: string, name: string, extras = 0, voice = false): CharacterDto => ({
  id,
  name,
  description: "",
  avatar: { id: `${id}-a`, kind: "image" } as never,
  extras: Array.from({ length: extras }, (_, i) => ({ id: `${id}-e${i}` }) as never),
  voice: voice ? ({ id: `${id}-v` } as never) : null,
  createdAt: "",
  updatedAt: "",
});

describe("角色卡选择框的选择逻辑", () => {
  test("多选：点一下选中、再点取消，保持勾选顺序", () => {
    let sel: string[] = [];
    sel = toggleCharacter(sel, "b", false);
    sel = toggleCharacter(sel, "a", false);
    expect(sel).toEqual(["b", "a"]);
    expect(toggleCharacter(sel, "b", false)).toEqual(["a"]);
  });

  test("单选（重新选择已失效的节点）：点哪个就只选哪个；再点同一个取消", () => {
    expect(toggleCharacter(["a"], "b", true)).toEqual(["b"]);
    expect(toggleCharacter(["a"], "a", true)).toEqual([]);
  });

  test("搜索：名称包含关键字，忽略大小写与首尾空格；空关键字返回全部", () => {
    const cards = [card("1", "林晚"), card("2", "Alice"), card("3", "alice 二号")];
    expect(filterCharacters(cards, "  ALI ").map((c) => c.id)).toEqual(["2", "3"]);
    expect(filterCharacters(cards, "")).toBe(cards);
    expect(filterCharacters(cards, "不存在")).toEqual([]);
  });

  test("底部文案：多选显示数量，单选替换；没选时确认按钮禁用", () => {
    expect(characterPickerSummary([], false)).toEqual({
      confirmLabel: "放到画布",
      disabled: true,
    });
    expect(characterPickerSummary(["a", "b"], false)).toEqual({
      confirmLabel: "放到画布 · 2",
      disabled: false,
    });
    expect(characterPickerSummary(["a"], true)).toEqual({ confirmLabel: "替换", disabled: false });
  });
});

describe("planCharacterNodes：在双击位置排布角色卡节点", () => {
  let seq = 0;
  const plan = (n: number, labels: string[] = []) =>
    planCharacterNodes(
      Array.from({ length: n }, (_, i) => card(`c${i}`, `角色${i}`, 1, true)),
      { x: 100, y: 50 },
      labels,
      () => `n${++seq}`,
    );

  test("每张卡一个节点：只存 characterId 与 labelCache，不带素材字段", () => {
    const [node] = plan(1);
    expect(node.type).toBe("canvas");
    expect(node.position).toEqual({ x: 100, y: 50 });
    expect(node.data).toEqual({
      kind: "character",
      label: "角色0",
      characterId: "c0",
      labelCache: { name: "角色0", coverAssetId: "c0-a" },
    });
  });

  test("横向最多 4 列，节点框之间留 40px，超过换行", () => {
    const nodes = plan(5);
    const xs = nodes.map((n) => n.position.x);
    const step = CHARACTER_NODE_WIDTH + ASSET_STACK_GAP;
    expect(ASSET_STACK_COLUMNS).toBe(4);
    expect(xs.slice(0, 4)).toEqual([100, 100 + step, 100 + 2 * step, 100 + 3 * step]);
    expect(nodes[4].position).toEqual({ x: 100, y: 50 + CHARACTER_NODE_HEIGHT + ASSET_STACK_GAP });
  });

  test("角色卡节点的短边（宽）与 1:1 素材节点的边长一样，封面 3:4，下面再加 40px 声音条", () => {
    expect(CHARACTER_NODE_WIDTH).toBe(NODE_FRAME_SHORT_SIDE);
    expect(CHARACTER_NODE_WIDTH).toBe(nodeFrameSize(1).width);
    expect(CHARACTER_NODE_HEIGHT).toBe(nodeFrameSize(3 / 4).height + 40);
  });

  test("节点名与画布上已有的重名时编号去重", () => {
    const nodes = plan(2, ["角色0"]);
    expect(nodes.map((n) => n.data.label)).toEqual(["角色0 2", "角色1"]);
  });
});
