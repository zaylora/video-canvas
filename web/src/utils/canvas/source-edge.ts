import { ANIMATED_EDGE_DATA } from "@/constants/canvas";
import type { CanvasEdge } from "@/types";

/** 来源线的 xyflow 边类型：外观和普通连线完全一样（同一个组件、同一份流动光带数据），只用来区分语义 */
export const SOURCE_EDGE_TYPE = "sourceEdge" as const;

/** 来源线存进画布 payload 时的 relation 取值；普通连线没有这个字段 */
export const SOURCE_RELATION = "source" as const;

/**
 * 来源线：记录「这个节点是从那个节点派生出来的」（比如从视频截出来的帧图），
 * 只用于画布上看清出处，不参与生成，不会被当成参考素材。
 * 画布里所有「按入边找上游素材」的地方都要先用 withoutSourceEdges 把它去掉。
 */
export const isSourceEdge = (edge: { type?: string }): boolean => edge.type === SOURCE_EDGE_TYPE;

/**
 * 去掉来源线，只留下参与生成的连线。没有来源线时原样返回同一个数组，免得白白触发重渲染。
 */
export function withoutSourceEdges<T extends { type?: string }>(edges: readonly T[]): T[] {
  const kept = edges.filter((edge) => !isSourceEdge(edge));
  return kept.length === edges.length ? (edges as T[]) : kept;
}

/**
 * 造一根来源线。
 * @param id 连线 id
 * @param source 被派生的节点（比如视频）
 * @param target 派生出来的节点（比如帧图）
 */
export function makeSourceEdge(id: string, source: string, target: string): CanvasEdge {
  return {
    id,
    source,
    target,
    sourceHandle: null,
    targetHandle: null,
    type: SOURCE_EDGE_TYPE,
    data: ANIMATED_EDGE_DATA,
  };
}
