import type { XYPosition } from "@xyflow/react";

import type { CharacterDto } from "@/api/character/type";
import { ASSET_STACK_COLUMNS, ASSET_STACK_GAP, NODE_META } from "@/constants/canvas";
import type { CanvasNode } from "@/types";

import { newNodeLabel } from "./node-label";

/** 角色卡节点卡片宽度（像素）：竖版 3:4 封面，比素材节点窄 */
export const CHARACTER_NODE_WIDTH = 216;

/** 角色卡节点框的高度（像素）：3:4 封面 + 底部声音条，排布时按它算行高 */
export const CHARACTER_NODE_HEIGHT = 328;

/**
 * 把选中的角色卡排成一批节点：每张卡一个节点，数据只有 characterId 和占位用的 labelCache
 * （内容实时取自角色卡，不随画布保存）。从落点起横向最多 ASSET_STACK_COLUMNS 列，
 * 节点框之间留 ASSET_STACK_GAP，超过一行换行，整批由调用方一次性写进画布（一条撤销记录）。
 * @param cards 选中的角色卡，按选择顺序
 * @param origin 落点：第一个节点的左上角（画布坐标）
 * @param existingLabels 画布里已有的节点名，用来给新节点编号去重
 * @param newId 节点 id 生成器，测试时注入
 * @returns 与 cards 等长、同序的节点
 */
export function planCharacterNodes(
  cards: CharacterDto[],
  origin: XYPosition,
  existingLabels: Iterable<string>,
  newId: () => string = () => crypto.randomUUID(),
): CanvasNode[] {
  const labels = new Set(existingLabels);
  const base = NODE_META.get("character")?.label ?? "角色卡";
  return cards.map((card, index) => {
    const column = index % ASSET_STACK_COLUMNS;
    const row = Math.floor(index / ASSET_STACK_COLUMNS);
    const label = newNodeLabel(card.name || base, labels);
    labels.add(label);
    return {
      id: newId(),
      type: "canvas",
      position: {
        x: origin.x + column * (CHARACTER_NODE_WIDTH + ASSET_STACK_GAP),
        y: origin.y + row * (CHARACTER_NODE_HEIGHT + ASSET_STACK_GAP),
      },
      data: {
        kind: "character",
        label,
        characterId: card.id,
        labelCache: {
          name: card.name,
          ...(card.avatar ? { coverAssetId: card.avatar.id } : {}),
        },
      },
    };
  });
}
