import { REMOTE_KIND_OF_NODE } from "@/constants/canvas";
import { useModelsStore } from "@/store/models";
import type { CanvasNodeData } from "@/types";
import { acceptsSourceKind, currentOp, readParams } from "@/utils/tasks/capabilities";

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
 * 先看节点种类规则（比如音频不能接进文本），再看下游当前模型和生成方式收不收这种上游
 * （比如「文生视频」不收图片）。拖线、落在节点上、落空建节点、倾斜反馈都用同一套。
 * 读模型清单不订阅，拖线时每帧调用也不会引起重渲染。
 */
export function canLinkNodes(source: LinkEnd, target: LinkEnd): boolean {
  if (!canConnectKinds(source.kind, "source", target.kind)) return false;
  const caps = capabilitiesOf(target);
  return acceptsSourceKind(caps, currentOp(caps, readParams(target)), source.kind);
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
