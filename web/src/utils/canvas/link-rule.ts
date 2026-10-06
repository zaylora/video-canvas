import { REMOTE_KIND_OF_NODE } from "@/constants/canvas";
import { useModelsStore } from "@/store/models";
import type { CanvasNodeData } from "@/types";
import type { GenerationOp } from "@/api/model/type";
import {
  acceptsSourceKind,
  currentOp,
  opToAcceptSource,
  readParams,
} from "@/utils/tasks/capabilities";

import { canConnectKinds } from "./canvas";

/** 判断连线只要节点的这几项 */
export type LinkEnd = Pick<CanvasNodeData, "kind" | "model" | "params" | "prompt">;

/** 下游节点当前模型的能力：节点没选模型就按清单第一条，清单还没到返回 undefined */
function capabilitiesOf(node: LinkEnd) {
  const models = useModelsStore.getState().byKind[REMOTE_KIND_OF_NODE[node.kind]]?.models ?? [];
  const model = models.find((item) => item.key === node.model) ?? models[0];
  return model?.capabilities;
}

/**
 * 上游能不能连到下游（节点只有一个输入口，所有连线都走这里判断）：
 * 先看节点种类规则（比如音频不能接进文本），再看下游的模型有没有哪种生成方式收这种上游。
 * 当前方式不收但换一种就收的（「文生视频」接图片），也放行，连上后由 opForLink 切方式。
 * 拖线、落在节点上、落空建节点、倾斜反馈都用同一套。
 * 读模型清单不订阅，拖线时每帧调用也不会引起重渲染。
 */
export function canLinkNodes(source: LinkEnd, target: LinkEnd): boolean {
  if (!canConnectKinds(source.kind, "source", target.kind)) return false;
  const caps = capabilitiesOf(target);
  const op = currentOp(caps, readParams(target));
  return acceptsSourceKind(caps, op, source.kind) || !!opToAcceptSource(caps, op, source.kind);
}

/**
 * 连上这根线时，下游要切到哪种生成方式才收得下上游（比如文生视频 → 全能参考）。
 * 不用切（本来就收、接不上）返回 undefined。
 */
export function opForLink(source: LinkEnd, target: LinkEnd): GenerationOp | undefined {
  if (!canConnectKinds(source.kind, "source", target.kind)) return undefined;
  const caps = capabilitiesOf(target);
  return opToAcceptSource(caps, currentOp(caps, readParams(target)), source.kind);
}

/** 从 from 节点的某一端拉线，落到 other 节点上能不能接：source 端拉出时 other 是下游 */
export function canLinkFrom(
  from: LinkEnd,
  handleType: "source" | "target",
  other: LinkEnd,
): boolean {
  return handleType === "source" ? canLinkNodes(from, other) : canLinkNodes(other, from);
}

/**
 * 多个节点一起引用到一个新节点上：按 canLinkNodes 把能接的和接不上的分开。
 * 多选拉出新节点时，接不上的节点不接线，只告诉用户有几个被跳过。
 */
export function partitionLinkable<T extends LinkEnd>(sources: T[], target: LinkEnd) {
  const linkable: T[] = [];
  const skipped: T[] = [];
  for (const source of sources) (canLinkNodes(source, target) ? linkable : skipped).push(source);
  return { linkable, skipped };
}

/** 判断能不能被引用要看的字段 */
export type MaterialEnd = LinkEnd & Pick<CanvasNodeData, "text" | "src" | "assetId" | "status">;

/**
 * 节点手里有没有能被引用的东西：文本要有正文，图片 / 视频 / 音频要有已经入库的素材。
 * 还在生成的不算，产物还没定。
 */
export function hasMaterial(
  data: Pick<MaterialEnd, "kind" | "text" | "src" | "assetId" | "status">,
) {
  if (data.status === "running") return false;
  return data.kind === "script" ? !!data.text?.trim() : !!data.src && !!data.assetId;
}

/** 从 start 顺着连线往下能走到的所有节点（不含 start） */
function downstreamOf(start: string, edges: readonly { source: string; target: string }[]) {
  const seen = new Set<string>();
  const stack = [start];
  while (stack.length > 0) {
    const current = stack.pop() as string;
    for (const edge of edges) {
      if (edge.source !== current || seen.has(edge.target) || edge.target === start) continue;
      seen.add(edge.target);
      stack.push(edge.target);
    }
  }
  return seen;
}

/**
 * 提示词里 @ 能列出的素材（设计稿 6.7）：已经连上的放 linked，画布里还没连、选了会自动连线的放 canvas。
 * 两组都只收：不是自己、有素材、canLinkNodes 接得上、不在自己下游（连回来会成环）。
 * 接不上的一律不返回，菜单里也就不显示。
 */
export function mentionableNodes<T extends { id: string; data: MaterialEnd }>(
  targetId: string,
  nodes: readonly T[],
  edges: readonly { source: string; target: string }[],
) {
  const target = nodes.find((node) => node.id === targetId);
  if (!target) return { linked: [] as T[], canvas: [] as T[] };
  const ok = (node: T) =>
    node.id !== targetId && hasMaterial(node.data) && canLinkNodes(node.data, target.data);
  const linkedIds = new Set(
    edges.filter((edge) => edge.target === targetId).map((edge) => edge.source),
  );
  const below = downstreamOf(targetId, edges);
  const byId = new Map(nodes.map((node) => [node.id, node]));
  return {
    linked: [...linkedIds].flatMap((id) => {
      const node = byId.get(id);
      return node && ok(node) ? [node] : [];
    }),
    canvas: nodes.filter((node) => ok(node) && !linkedIds.has(node.id) && !below.has(node.id)),
  };
}

/**
 * 断开某个上游连到 targetId 的线（引用条上点 ×）。同一对节点有几根就都删；
 * 一根都没有时返回原数组，免得多记一步撤销、多存一次。
 */
export function unlinkSource<T extends { source: string; target: string }>(
  edges: T[],
  sourceId: string,
  targetId: string,
): T[] {
  const kept = edges.filter((edge) => edge.source !== sourceId || edge.target !== targetId);
  return kept.length === edges.length ? edges : kept;
}
