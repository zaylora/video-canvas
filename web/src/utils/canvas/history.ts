import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";

import { selectOutput } from "./outputs";

/** 撤销栈最多记几步 */
export const HISTORY_LIMIT = 100;

/** 一步撤销记下的画布 */
export type HistorySnapshot = { nodes: CanvasNode[]; edges: CanvasEdge[] };

/**
 * 由生成任务写进来的字段：撤销不该把它们倒回去，
 * 否则撤一步就把刚生成好的结果、正在跑的任务状态一起抹掉了。
 */
const TASK_FIELDS = [
  "status",
  "taskId",
  "error",
  "text",
  "outputs",
  "src",
  "assetId",
  "mediaType",
  "uploaded",
  "fileName",
] as const satisfies readonly (keyof CanvasNodeData)[];

/** 节点的结构：增删、位置、连线变了就是一步，立刻入栈 */
export function structureKey({ nodes, edges }: HistorySnapshot) {
  return JSON.stringify([
    nodes.map((node) => [node.id, Math.round(node.position.x), Math.round(node.position.y)]),
    // 连接点（handle）不算：换模型后节点会自己把线挪到新口上，那不是用户的一步
    edges.map((edge) => [edge.id, edge.source, edge.target]),
  ]);
}

/** 节点的内容：标题、模型、提示词、参数。连着打字只算一步，要攒一会儿再入栈 */
export function contentKey({ nodes }: HistorySnapshot) {
  return JSON.stringify(
    nodes.map(({ data }) => [data.label, data.model, data.prompt, data.params, data.paramAssets]),
  );
}

/**
 * 撤销 / 重做时把快照里的节点放回画布：用户改的（位置、标题、参数、当前版本）取快照里的，
 * 任务写的（状态、产物、历史版本）取眼下的；当前版本按眼下的历史重新对一遍镜像字段。
 * 快照里有、眼下已经被删的节点原样放回。
 */
export function restoreNodes(snapshot: CanvasNode[], current: CanvasNode[]): CanvasNode[] {
  const live = new Map(current.map((node) => [node.id, node]));
  return snapshot.map((saved) => {
    const now = live.get(saved.id);
    if (!now) return saved;
    const data: CanvasNodeData = { ...saved.data };
    for (const key of TASK_FIELDS) {
      if (now.data[key] === undefined) delete data[key];
      else Object.assign(data, { [key]: now.data[key] });
    }
    const mirrored =
      saved.data.activeOutputId && now.data.status !== "running"
        ? selectOutput(data, saved.data.activeOutputId)
        : null;
    return {
      ...saved,
      measured: now.measured,
      width: now.width,
      height: now.height,
      data: mirrored
        ? { ...data, ...mirrored }
        : { ...data, activeOutputId: now.data.activeOutputId },
    };
  });
}
