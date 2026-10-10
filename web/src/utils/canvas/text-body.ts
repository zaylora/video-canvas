import type { CanvasNodeData } from "@/types";

/**
 * 双击编辑完文本节点正文后，要写回节点数据的改动。
 * 写了内容就算有结果（done），清空了回到没内容（idle），之前的失败信息一并去掉。
 * @param data 节点现在的数据
 * @param draft 编辑框里的内容
 * @returns 改动；内容没变返回 null
 */
export function textEditPatch(
  data: Pick<CanvasNodeData, "text">,
  draft: string,
): Partial<CanvasNodeData> | null {
  if (draft === (data.text ?? "")) return null;
  return {
    text: draft,
    status: draft.trim() ? "done" : "idle",
    error: null,
    errorTaskRef: null,
  };
}

/**
 * 下游引用一个节点时读到的文字：就是文本节点正文区的内容，不是它的提示词。
 * 正在生成时正文还是上一版，引用了会和即将回填的内容对不上，所以不引用。
 * @param data 被引用节点的数据
 * @returns 正文；没有或正在生成为 undefined
 */
export function referencedText(
  data: Pick<CanvasNodeData, "status" | "text" | "prompt">,
): string | undefined {
  return data.status === "running" ? undefined : (data.text ?? undefined);
}
