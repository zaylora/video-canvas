import type { Capabilities, ModelInfo } from "@/api/model/type";
import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import { NODE_LABEL_MAX, newNodeLabel } from "./node-label";
import { nodeFrameSize, parseAspect, ratioParam } from "./node-frame";
import { snapRatio } from "./outpaint-frame";
import { placeBeside, sizeOfNode } from "./place-beside";

/** 拼在用户提示词前面的固定指令：告诉模型只补透明区域、别动原图（设计稿 6.17） */
export const OUTPAINT_INSTRUCTION = "保持原图内容不变，自然补全图片中的透明区域。";

/**
 * 扩图提示词 = 固定指令 + 用户写的内容（可以为空）。
 * 完整提示词会写进新节点，这样在新节点点「重新生成」结果和第一次一致。
 */
export function buildOutpaintPrompt(userPrompt: string): string {
  return OUTPAINT_INSTRUCTION + userPrompt.trim();
}

/**
 * 扩图要建的新节点和它到原图的连线。
 * 节点是「生成中」的图片节点（还没有任务，合成和上传完成后再写 taskId），参数里预置图生图、完整提示词和比例；
 * 合成图的素材 id 上传完才有，由调用方之后补进 params.images。
 * 摆在原图右侧 80px 外、垂直居中，撞到别的节点就往下错开。
 * @param input.ratio 要写进模型比例参数的值，如 "16:9"
 * @param input.ratioKey 模型比例参数的名字；模型没有比例参数时为 undefined，不写比例
 * @param input.prompt 用户写的提示词（不含固定指令）
 * @param input.newId 生成节点 id（测试里可注入）
 * @param input.edgeId 生成连线 id
 */
export function buildOutpaintNode(input: {
  source: CanvasNode;
  nodes: FlowNode[];
  model: string;
  prompt: string;
  ratio: string;
  ratioKey: string | undefined;
  newId?: () => string;
  edgeId?: () => string;
}): { node: CanvasNode; edge: CanvasEdge } {
  const { source, nodes, model, ratio, ratioKey } = input;
  const newId = input.newId ?? (() => crypto.randomUUID());
  const edgeId = input.edgeId ?? (() => crypto.randomUUID());

  const baseLabel = `${[...source.data.label].slice(0, NODE_LABEL_MAX - 5).join("")}-扩图`;
  const label = newNodeLabel(
    baseLabel,
    nodes.map((node) => node.data.label),
  );

  const size = sizeOfNode(source);
  const aspect = parseAspect(ratio) ?? size.width / size.height;
  const newSize = nodeFrameSize(aspect);
  const { x, y } = placeBeside({ source, nodes, size: newSize });

  const fullPrompt = buildOutpaintPrompt(input.prompt);
  const id = newId();
  const node: CanvasNode = {
    id,
    type: "canvas",
    position: { x, y },
    data: {
      kind: "image",
      label,
      model,
      prompt: fullPrompt,
      params: { prompt: fullPrompt, op: "i2i", ...(ratioKey ? { [ratioKey]: ratio } : {}) },
      status: "running",
      mediaAspect: aspect,
      outpaintFrom: source.id,
    },
  };
  const edge: CanvasEdge = {
    id: edgeId(),
    source: source.id,
    target: id,
    sourceHandle: null,
    targetHandle: null,
    ...ANIMATED_EDGE_OPTIONS,
  };
  return { node, edge };
}

/** 能做扩图的模型：支持图生图，并且开了图片参考 */
export function outpaintModels(models: ModelInfo[]): ModelInfo[] {
  return models.filter(
    (model) => !!model.capabilities.ops?.includes("i2i") && !!model.capabilities.refs?.image?.on,
  );
}

/**
 * 扩图默认用哪个模型：原图节点的模型能做扩图就沿用，否则取第一个能做的。
 * @param models 全部图片模型
 * @param preferred 原图节点选的模型 key
 * @returns 一个都没有时为 undefined
 */
export function pickOutpaintModel(
  models: ModelInfo[],
  preferred: string | undefined,
): ModelInfo | undefined {
  const usable = outpaintModels(models);
  return usable.find((model) => model.key === preferred) ?? usable[0];
}

/**
 * 比例要写进模型的哪个参数、写什么值。参数是枚举时取最接近的选项（模型不支持的比例也不会被后端拒绝）；
 * 模型没有比例参数时 key 为 undefined，不写。
 * @param ratio 框的比例，如 "16:9"
 */
export function resolveRatioParam(
  caps: Capabilities | undefined,
  ratio: string,
): { key: string | undefined; value: string } {
  const field = ratioParam(caps);
  if (!field) return { key: undefined, value: ratio };
  return {
    key: field.name,
    value: field.options?.length ? snapRatio(ratio, field.options) : ratio,
  };
}

/**
 * 扩图出来的节点，组装参考图时不算来自原图的那条连线：它只表示来源，参考图只有合成图。
 * 不是扩图出来的节点（outpaintFrom 为空）原样返回同一个数组，免得白白换引用引起重算。
 * @param connections 接进这个节点的连线
 * @param outpaintFrom 节点数据里记的原图节点 id
 */
export function withoutOutpaintSource<T extends { source: string }>(
  connections: T[],
  outpaintFrom: string | undefined,
): T[] {
  if (!outpaintFrom) return connections;
  return connections.filter((connection) => connection.source !== outpaintFrom);
}
