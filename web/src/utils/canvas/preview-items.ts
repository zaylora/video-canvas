import type { CanvasNode } from "@/types";

/** 节点没测量出高度时按这个值估算行高容差的基准 */
const FALLBACK_NODE_HEIGHT = 270;

/** 预览弹层里的一项：画布上一个有图片或视频的节点 */
export type PreviewItem = {
  /** 节点 id，定位到节点、被删后回退都靠它 */
  id: string;
  /** 素材地址（payload 里的稳定地址，不带变体） */
  src: string;
  mediaType: "image" | "video";
  /** 节点名，下载文件名和无障碍说明用 */
  label: string;
  /** 上传素材的原文件名 */
  fileName?: string;
  /** 节点在画布上的位置，只用来排序 */
  x: number;
  y: number;
};

/**
 * 收集画布上可以预览的节点，按「先行后列」排好：从上到下一行行排，同一行内从左到右。
 * 为什么要分行：节点常常摆得略有错位，直接按 y 排会让右边稍高的节点跑到左边前面，
 * 所以 y 相差不到半个节点高度就算同一行。
 * 音频、文本节点和还没出结果的节点不进列表。
 * @param nodes 画布上全部节点
 * @returns 可预览项，顺序即翻页顺序
 */
export function collectPreviewItems(nodes: CanvasNode[]): PreviewItem[] {
  const picked: Array<PreviewItem & { height: number }> = [];
  for (const node of nodes) {
    const { src, mediaType, kind, label, fileName } = node.data;
    if (!src || (mediaType !== "image" && mediaType !== "video")) continue;
    if (kind !== "image" && kind !== "video") continue;
    picked.push({
      id: node.id,
      src,
      mediaType,
      label,
      fileName,
      x: node.position.x,
      y: node.position.y,
      height: node.measured?.height ?? FALLBACK_NODE_HEIGHT,
    });
  }

  picked.sort((a, b) => a.y - b.y);
  const rows: Array<typeof picked> = [];
  let rowTop = Number.NEGATIVE_INFINITY;
  let tolerance = 0;
  for (const item of picked) {
    if (rows.length === 0 || item.y - rowTop > tolerance) {
      rows.push([]);
      rowTop = item.y;
      tolerance = item.height / 2;
    }
    rows[rows.length - 1]!.push(item);
  }

  return rows.flatMap((row) =>
    row.sort((a, b) => a.x - b.x).map(({ height: _height, ...item }) => item),
  );
}

/**
 * 翻页：到头就停住，不循环（缩略图条有位置感，循环会让人迷路）。
 * @param index 当前位置
 * @param delta 方向，-1 上一个，1 下一个
 * @param length 列表长度
 */
export function stepPreviewIndex(index: number, delta: -1 | 1, length: number): number {
  return Math.min(Math.max(index + delta, 0), length - 1);
}

/**
 * 列表变化后重算当前位置：当前项还在就取它的新位置；
 * 被删或素材被换掉了，落到原位置上的相邻项（越界取最后一项）。
 * @param items 最新的可预览项
 * @param activeId 之前正在看的节点
 * @param lastIndex 之前的位置
 * @returns 新位置；列表空了返回 -1，调用方应关闭弹层
 */
export function resolveActiveIndex(
  items: PreviewItem[],
  activeId: string,
  lastIndex: number,
): number {
  if (items.length === 0) return -1;
  const found = items.findIndex((item) => item.id === activeId);
  return found >= 0 ? found : Math.min(lastIndex, items.length - 1);
}

/**
 * 文件大小转成信息胶囊里的文字。
 * @param bytes 字节数
 * @returns 如 `11.3 MB`；没有有效大小返回 null
 */
export function formatFileSize(bytes: number | null | undefined): string | null {
  if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes <= 0) return null;
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  if (bytes < 1024 ** 2) return `${Math.round(bytes / 1024)} KB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
}
