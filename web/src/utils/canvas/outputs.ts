import type { CanvasNodeData, NodeKind, NodeOutput } from "@/types";

/** 每个节点最多留几版，超出丢最旧的（只丢引用，素材本身不删） */
export const MAX_NODE_OUTPUTS = 20;

/**
 * 节点的历次产物。旧画布没有 outputs，但有一份已经存下来的素材时，就把它当成唯一的一版；
 * 本地还没传完的 blob、没有 assetId 的素材不算。
 */
export function readOutputs(data: CanvasNodeData): NodeOutput[] {
  if (data.outputs?.length) return data.outputs;
  if (!data.src || !data.assetId || data.src.startsWith("blob:")) return [];
  return [
    {
      id: data.assetId,
      src: data.src,
      mediaType: data.mediaType ?? "image",
      assetId: data.assetId,
      taskId: data.taskId,
      model: data.model,
      createdAt: 0,
    },
  ];
}

/** 把一版产物挂成当前版本；同一素材已经在列里就只切过去，不重复入列 */
export function appendOutput(
  data: CanvasNodeData,
  output: NodeOutput,
): Pick<CanvasNodeData, "outputs" | "activeOutputId" | "src" | "mediaType" | "assetId"> {
  const current = readOutputs(data);
  const outputs = current.some((item) => item.id === output.id)
    ? current
    : [...current, output].slice(-MAX_NODE_OUTPUTS);
  return {
    outputs,
    activeOutputId: output.id,
    src: output.src,
    mediaType: output.mediaType,
    assetId: output.assetId,
  };
}

/** 切到某一版：只改当前指向和镜像字段，找不到这一版时返回 null */
export function selectOutput(
  data: CanvasNodeData,
  outputId: string,
): Pick<CanvasNodeData, "outputs" | "activeOutputId" | "src" | "mediaType" | "assetId"> | null {
  const outputs = readOutputs(data);
  const output = outputs.find((item) => item.id === outputId);
  if (!output) return null;
  return {
    outputs,
    activeOutputId: output.id,
    src: output.src,
    mediaType: output.mediaType,
    assetId: output.assetId,
  };
}

/** 当前那一版的 id：没记的时候认最后一版 */
export function activeOutputIdOf(data: CanvasNodeData): string | undefined {
  const outputs = readOutputs(data);
  if (data.activeOutputId && outputs.some((item) => item.id === data.activeOutputId))
    return data.activeOutputId;
  return outputs.at(-1)?.id;
}

/** 一版素材拖出来建节点时该建成哪种节点 */
export const MEDIA_KIND_OF = {
  image: "image",
  video: "video",
  audio: "audio",
} as const satisfies Record<NodeOutput["mediaType"], NodeKind>;
