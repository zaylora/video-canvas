import type { XYPosition } from "@xyflow/react";

import type { AssetDto } from "@/api/asset/type";
import {
  ASSET_STACK_COLUMNS,
  ASSET_STACK_GAP,
  NODE_LIBRARY,
  NODE_META,
  UPLOAD_TARGET_KIND,
} from "@/constants/canvas";
import type { CanvasNode } from "@/types";

import { nodeFrameSize, resolveNodeAspect } from "./node-frame";
import { newNodeLabel, uploadLabel } from "./node-label";

/**
 * 素材自带的宽高比；音频没有画面、宽高缺失或为 0 的素材返回 undefined，节点按默认画幅占位。
 * @param asset 素材
 */
function assetAspect(asset: AssetDto): number | undefined {
  if (asset.kind === "audio") return undefined;
  const { width, height } = asset;
  return width && height && width > 0 && height > 0 ? width / height : undefined;
}

/**
 * 把从资产库选中的素材排成一批节点：每个素材一个节点，数据和「上传素材」落成的节点一致
 * （uploaded、fileName、已有 assetId 和稳定地址，状态直接是 done，不触发上传）。
 * 从落点起横向最多 ASSET_STACK_COLUMNS 列，节点框之间留 ASSET_STACK_GAP；
 * 列宽、行高按各节点框的真实尺寸算（画幅不同的素材也不会叠在一起），超过一行就换行。
 * 返回的节点未选中，由调用方一次性写进画布（整批一条撤销记录）。
 * @param assets 选中的素材，按选择顺序
 * @param origin 落点：第一个节点的左上角（画布坐标）
 * @param existingLabels 画布里已有的节点名，用来给新节点编号去重
 * @param models 各种类新建时预选的模型 id，缺省由节点自己回落到清单第一条
 * @param newId 节点 id 生成器，测试时注入
 * @returns 与 assets 等长、同序的节点
 */
export function planAssetNodes(
  assets: AssetDto[],
  origin: XYPosition,
  existingLabels: Iterable<string>,
  newId: () => string = () => crypto.randomUUID(),
  models?: Record<string, string>,
): CanvasNode[] {
  const labels = new Set(existingLabels);
  const nodes: CanvasNode[] = [];
  /** 当前行下一个节点的 x（相对落点）与本行已排节点的最大高度 */
  let cursorX = 0;
  let rowTop = 0;
  let rowHeight = 0;

  assets.forEach((asset, index) => {
    if (index > 0 && index % ASSET_STACK_COLUMNS === 0) {
      rowTop += rowHeight + ASSET_STACK_GAP;
      cursorX = 0;
      rowHeight = 0;
    }

    const kind = UPLOAD_TARGET_KIND[asset.kind];
    const meta = NODE_META.get(kind) ?? NODE_LIBRARY[0];
    const mediaAspect = assetAspect(asset);
    const frame = nodeFrameSize(
      resolveNodeAspect({
        measured: null,
        declared: mediaAspect,
        selected: null,
        mediaType: asset.kind,
      }),
    );

    const label = newNodeLabel(
      asset.fileName ? uploadLabel(asset.fileName, meta.label) : meta.label,
      labels,
    );
    labels.add(label);

    nodes.push({
      id: newId(),
      type: "canvas",
      position: { x: origin.x + cursorX, y: origin.y + rowTop },
      data: {
        kind: meta.kind,
        label,
        model: models?.[meta.kind],
        status: "done",
        src: asset.url,
        mediaType: asset.kind,
        assetId: asset.id,
        uploaded: true,
        ...(asset.fileName ? { fileName: asset.fileName } : {}),
        ...(mediaAspect !== undefined ? { mediaAspect } : {}),
      },
    });

    cursorX += frame.width + ASSET_STACK_GAP;
    rowHeight = Math.max(rowHeight, frame.height);
  });

  return nodes;
}
