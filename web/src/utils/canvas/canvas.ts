import type { HandleType } from "@xyflow/react";

import {
  DOWNSTREAM_KINDS,
  isRemoteModelKind,
  MODEL_LIBRARY,
  NODE_LIBRARY,
} from "@/constants/canvas";
import type { CustomModel } from "@/store";
import type { ModelOption, NodeKind } from "@/types";

/** 从某个节点的某一端拉线时，允许新建的种类；从 target 端拉线要反查谁能生成它 */
export function getAllowedKinds(
  kind: NodeKind,
  handleType: HandleType,
): NodeKind[] {
  if (handleType === "source") return DOWNSTREAM_KINDS[kind];

  return NODE_LIBRARY.filter((meta) =>
    DOWNSTREAM_KINDS[meta.kind].includes(kind),
  ).map((meta) => meta.kind);
}

/**
 * 从某个节点的某一端拉出来的线，能不能接到另一个种类的节点上。
 * 从 source 端拉出时对方是下游，从 target 端拉出时对方得是能生成自己的上游。
 */
export function canConnectKinds(
  fromKind: NodeKind,
  handleType: HandleType,
  toKind: NodeKind,
): boolean {
  return handleType === "source"
    ? DOWNSTREAM_KINDS[fromKind].includes(toKind)
    : DOWNSTREAM_KINDS[toKind].includes(fromKind);
}

/** 取某种类可选的模型 */
export function getModels(kind: NodeKind): readonly ModelOption[] {
  return MODEL_LIBRARY[kind];
}

/** 自定义模型按输入框认得的形状摊平，下拉里的小字给模型标识 */
function toOption(model: CustomModel): ModelOption {
  return {
    id: model.id,
    label: model.label,
    credits: model.credits,
    hint: model.modelId,
  };
}

/**
 * 某种类真正能挑的模型：内置清单加上设置里接进来的那些。
 * 服务端下发清单的种类（视频）只认 remote，不混本地演示项，也不支持自定义模型。
 */
export function getModelOptions(
  kind: NodeKind,
  customModels: readonly CustomModel[],
  remote: readonly ModelOption[] = [],
): ModelOption[] {
  if (isRemoteModelKind(kind)) return [...remote];
  return [
    ...getModels(kind),
    ...customModels.filter((model) => model.kind === kind).map(toOption),
  ];
}

/** 取当前该用的模型：存的那个已经删了就退回清单第一条 */
export function pickModel(
  options: readonly ModelOption[],
  id?: string,
): ModelOption {
  return options.find((model) => model.id === id) ?? options[0];
}
