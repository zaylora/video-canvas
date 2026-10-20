import type { CanvasEdge, CanvasNodeData, FlowNode } from "@/types";

import { applyChanges, type GraphChange, type MergeEdge, type MergeOptions } from "./graph-changes";
import { isGroupNode } from "./group";
import { selectOutput } from "./outputs";

/** 撤销栈最多记几步 */
export const HISTORY_LIMIT = 100;

/** 一步撤销记下的画布 */
export type HistorySnapshot = { nodes: FlowNode[]; edges: CanvasEdge[] };

/**
 * 由生成任务写进来的字段：撤销不该把它们倒回去，
 * 否则撤一步就把刚生成好的结果、正在跑的任务状态一起抹掉了。
 */
const TASK_FIELDS = [
  "status",
  "uploadProgress",
  "taskId",
  "pendingTaskIds",
  "error",
  "text",
  "outputs",
  "src",
  "assetId",
  "mediaType",
  "mediaAspect",
  "uploaded",
  "fileName",
] as const satisfies readonly (keyof CanvasNodeData)[];

/**
 * 导演台场景：编辑器有自己的撤销栈，画布撤销（比如撤销挪节点）不能把场景倒回快照时的样子。
 */
const LIVE_FIELDS = [...TASK_FIELDS, "scene"] as const satisfies readonly (keyof CanvasNodeData)[];

/** 节点的结构：增删、位置、连线变了就是一步，立刻入栈 */
export function structureKey({ nodes, edges }: HistorySnapshot) {
  return JSON.stringify([
    nodes.map((node) => [
      node.id,
      Math.round(node.position.x),
      Math.round(node.position.y),
      // 入组 / 退组、缩放组框也是一步
      node.parentId ?? null,
      isGroupNode(node) ? [Math.round(node.width ?? 0), Math.round(node.height ?? 0)] : null,
    ]),
    // 连接点（handle）不算：换模型后节点会自己把线挪到新口上，那不是用户的一步
    edges.map((edge) => [edge.id, edge.source, edge.target]),
  ]);
}

/** 节点的内容：标题、模型、提示词、参数。连着打字只算一步，要攒一会儿再入栈 */
export function contentKey({ nodes }: HistorySnapshot) {
  return JSON.stringify(
    nodes.map((node) =>
      isGroupNode(node)
        ? [node.data.label, node.data.color, node.data.labelColor]
        : [
            node.data.label,
            node.data.model,
            node.data.prompt,
            node.data.params,
            node.data.paramAssets,
          ],
    ),
  );
}

/**
 * 撤销 / 重做时把快照里的节点放回画布：用户改的（位置、标题、参数、当前版本）取快照里的，
 * 任务写的（状态、产物、历史版本）取眼下的；当前版本按眼下的历史重新对一遍镜像字段。
 * 快照里有、眼下已经被删的节点原样放回。
 */
export function restoreNodes(snapshot: FlowNode[], current: FlowNode[]): FlowNode[] {
  const live = new Map(current.map((node) => [node.id, node]));
  return snapshot.map((saved): FlowNode => {
    const now = live.get(saved.id);
    if (!now) return saved;
    // 组没有任务字段：位置、尺寸、名字、颜色全取快照，只保留眼下测得的尺寸
    if (isGroupNode(saved) || isGroupNode(now)) return { ...saved, measured: now.measured };
    const data: CanvasNodeData = { ...saved.data };
    for (const key of LIVE_FIELDS) {
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

/** 把 Agent 的改动并进快照时造节点、造连线、整理顺序的方式（和并进画布时用的同一套） */
export type RebaseOptions = Pick<
  MergeOptions<FlowNode, CanvasEdge & MergeEdge>,
  "makeNode" | "makeEdge" | "normalize"
>;

/**
 * 把一批 Agent 的改动并进撤销栈 / 重做栈里的一张快照：撤销用户自己的步骤时，不能把 Agent 刚改的内容倒回去。
 * 规则和并进画布一致：节点字段只有快照里还等于「改动前」才换成「改动后」（用户自己的值保持）；
 * 新建的节点、连线加进快照（连线两端不在快照里就跳过）；删除的节点连同连着它的线从快照里去掉。
 * 改不到这张快照时原样返回同一个对象，不改入参。
 * @param snapshot 栈里的一张快照
 * @param changes 并进画布的那批改动
 * @param merge 造节点、造连线、整理顺序
 */
export function rebaseSnapshot(
  snapshot: HistorySnapshot,
  changes: GraphChange[],
  merge: RebaseOptions,
): HistorySnapshot {
  const result = applyChanges<FlowNode, CanvasEdge & MergeEdge>(snapshot, changes, {
    guard: true,
    ...merge,
  });
  if (result.applied === 0) return snapshot;
  return { nodes: result.nodes, edges: result.edges };
}
