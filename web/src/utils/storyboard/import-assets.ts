import type { CanvasNodeData, NodeStoryboard } from "@/types";
import { activeOutputIdOf, readOutputs } from "@/utils/canvas/outputs";

import { addShot, linkShot, updateShot } from "./shots";

/** 能导入分镜表的素材种类：视频挂「视频」列，分镜图挂「分镜图」列 */
export type ImportKind = "video" | "image";

/** 画布上的一个节点，只取排序和展示要用的字段（画布内部节点同样满足） */
export type AssetNode = {
  id: string;
  position: { x: number; y: number };
  data: CanvasNodeData;
};

/** 一个可以导入分镜表的画布素材 */
export type CanvasAsset = {
  /** 节点 id，导入后写进镜头的 videoNodeId / imageNodeId */
  id: string;
  /** 节点标题，列表里用来认人 */
  label: string;
  /** 画布顺序，1 起：上→下、同高左→右 */
  seq: number;
  /** 有没有能用的产物：视频要出过片、图片要出过图。没有的导过去也合成不了，列表里标出来 */
  ready: boolean;
  /** 已经挂在哪几镜（镜号，1 起）；同一个节点可以被多镜复用，这里如实列出来 */
  usedBy: number[];
  /** 缩略图地址；视频取当前那一版素材的封面 */
  src?: string;
};

/** 要关联到某一镜的节点 */
export type ImportLink = { shotId: string; nodeId: string };

/** 一次导入的结果：写进哪些镜头、追加几行、几个挂不上 */
export type ImportPlan = {
  /** 挂到已有空镜头上的那些，按镜号顺序 */
  links: ImportLink[];
  /** 空镜头不够、要新追加的行（勾了「多出来的追加为镜头」才有） */
  appended: { nodeId: string; label: string }[];
  /** 既没有空镜头、也没勾追加，挂不上的数量 */
  leftover: number;
};

const KIND_NAME: Record<ImportKind, string> = { video: "视频", image: "分镜图" };

/** 素材种类的中文说法：视频 / 分镜图 */
export const importKindName = (kind: ImportKind): string => KIND_NAME[kind];

/** 某一镜上记的是哪种字段 */
const LINK_KEY: Record<ImportKind, "videoNodeId" | "imageNodeId"> = {
  video: "videoNodeId",
  image: "imageNodeId",
};

/**
 * 每个节点被哪些镜挂着（镜号从 1 起）。
 * 一个视频节点被两镜共用是合法的（同一段素材在前后的镜头里各出现一次），所以值是数组。
 */
export function usedByShots(board: NodeStoryboard, kind: ImportKind): Map<string, number[]> {
  const key = LINK_KEY[kind];
  const map = new Map<string, number[]>();
  board.shots.forEach((shot, index) => {
    const nodeId = shot[key];
    if (!nodeId) return;
    const list = map.get(nodeId);
    if (list) list.push(index + 1);
    else map.set(nodeId, [index + 1]);
  });
  return map;
}

/** 画布顺序：先上下、再左右，位置也一样时按 id 定死（和成片排序同一条规则） */
const byCanvasPosition = (a: AssetNode, b: AssetNode) =>
  a.position.y - b.position.y || a.position.x - b.position.x || a.id.localeCompare(b.id);

/**
 * 把画布上的视频 / 图片节点整理成可导入的候选：按画布位置排，标出有没有出片、已经挂在哪几镜。
 *
 * 顺序只认位置，不做任何内容推断 —— 列表里第一项就是画布上最靠上的那个，
 * 批量导入按这个顺序补镜头，用户看到的和在画布上看到的一致。
 */
export function canvasAssetsOf(
  nodes: readonly AssetNode[],
  kind: ImportKind,
  board: NodeStoryboard,
): CanvasAsset[] {
  const used = usedByShots(board, kind);
  return nodes
    .filter((node) => node.data.kind === kind)
    .sort(byCanvasPosition)
    .map((node, index) => {
      const outputs = readOutputs(node.data);
      const activeId = activeOutputIdOf(node.data);
      const output = outputs.find((item) => item.id === activeId);
      return {
        id: node.id,
        label: node.data.label,
        seq: index + 1,
        // 有 assetId 才算能用：只有本地 blob 的节点后端拿不到文件
        ready: !!output?.assetId,
        usedBy: used.get(node.id) ?? [],
        src: output?.src ?? node.data.src ?? undefined,
      };
    });
}

/** 还没挂这种素材的镜头下标（按行序），批量导入就往这些行里填 */
export function importTargets(board: NodeStoryboard, kind: ImportKind): number[] {
  const key = LINK_KEY[kind];
  const out: number[] = [];
  board.shots.forEach((shot, index) => {
    if (!shot[key]) out.push(index);
  });
  return out;
}

/** 默认勾选哪些：没被任何镜用过的，免得一打开就把已有安排覆盖一遍 */
export function defaultSelection(assets: readonly CanvasAsset[]): string[] {
  return assets.filter((asset) => asset.usedBy.length === 0).map((asset) => asset.id);
}

/**
 * 算这次导入会写成什么样：勾中的素材按顺序补进还没有素材的镜头；
 * 空镜头不够时，勾了 append 就追加成新行（画面描述先取节点标题），没勾就记成挂不上。
 * @param picked 勾中的素材，顺序就是挂载顺序
 */
export function planImport(
  board: NodeStoryboard,
  picked: readonly CanvasAsset[],
  kind: ImportKind,
  { append = false }: { append?: boolean } = {},
): ImportPlan {
  const targets = importTargets(board, kind);
  const plan: ImportPlan = { links: [], appended: [], leftover: 0 };
  picked.forEach((asset, index) => {
    const at = targets[index];
    if (at !== undefined) {
      plan.links.push({ shotId: board.shots[at].id, nodeId: asset.id });
    } else if (append) {
      plan.appended.push({ nodeId: asset.id, label: asset.label });
    } else {
      plan.leftover += 1;
    }
  });
  return plan;
}

/**
 * 追加一行镜头并把素材挂上：分镜表是后来加的、没有对应镜头时用。
 * 画面描述先取节点标题（「S1-v2」这种），用户随后可以改成真正的描述。
 * @param newId 新行 id，测试里换成可预测的
 */
export function appendLinkedShot(
  board: NodeStoryboard,
  nodeId: string,
  kind: ImportKind,
  label: string,
  newId: () => string = () => crypto.randomUUID(),
): NodeStoryboard {
  const added = addShot(board, newId);
  const id = added.shots[added.shots.length - 1].id;
  const named = updateShot(added, id, { scene: label });
  return linkShot(named, id, kind === "video" ? { videoNodeId: nodeId } : { imageNodeId: nodeId });
}
