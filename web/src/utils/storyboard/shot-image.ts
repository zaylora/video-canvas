import type { StoryboardShot } from "@/types";
import type { PreviewItem } from "@/utils/canvas/preview-items";
import type { VideoNodeView } from "@/utils/tasks/node-view";

import { formatShotNumber } from "./shots";

/** 关联的图片节点此刻的情况：不在画布上，或在，带上它的展示状态和标题 */
export type LinkedNode = { exists: false } | { exists: true; view: VideoNodeView; label?: string };

/** 分镜图格该显示什么（设计 §6.5）；「上传中」是格子自己的本地瞬态，不在这里 */
export type ShotImageState =
  | { kind: "empty" }
  | { kind: "uploaded"; src: string }
  | { kind: "node"; phase: "idle" }
  | { kind: "node"; phase: "queued" }
  | { kind: "node"; phase: "running"; progress: number | null }
  | { kind: "node"; phase: "done"; src: string }
  | { kind: "node"; phase: "failed"; message: string };

/**
 * 一行的分镜图格状态：手传的图优先；其次是关联的图片节点，实时读它（状态不在行上另存一份）；
 * 节点已被删（AC8）或根本没有来源就是空。
 * @param linked 关联节点的情况；这一行没有 imageNodeId 时传 undefined
 */
export function shotImageState(
  shot: Pick<StoryboardShot, "image" | "imageNodeId">,
  linked: LinkedNode | undefined,
): ShotImageState {
  if (shot.image) return { kind: "uploaded", src: shot.image.src };
  if (!shot.imageNodeId || !linked?.exists) return { kind: "empty" };
  const v = linked.view;
  switch (v.phase) {
    case "queued":
      return { kind: "node", phase: "queued" };
    case "running":
      return { kind: "node", phase: "running", progress: v.progress };
    // 即将完成（转存中）也算生成中，只是没有进度可说
    case "finalizing":
      return { kind: "node", phase: "running", progress: null };
    case "done":
      return { kind: "node", phase: "done", src: v.src };
    case "failed":
      return { kind: "node", phase: "failed", message: v.message };
    // 本地传图进来的节点没有生成任务，当作还没生成
    default:
      return { kind: "node", phase: "idle" };
  }
}

/**
 * 批量生成分镜图要处理哪些行：只补没图的。空白、关联节点还没出图、生成失败的生成；
 * 已有图（生成好的或手传的）、排队和生成中的跳过。画面描述是空的行没法拼提示词，不生成，单独计数。
 * @param linkedOf 取某一行关联节点的情况；没有 imageNodeId 时返回 undefined
 */
export function batchImagePlan(
  shots: StoryboardShot[],
  linkedOf: (shot: StoryboardShot) => LinkedNode | undefined,
): { targets: string[]; missingScene: number } {
  const targets: string[] = [];
  let missingScene = 0;
  for (const shot of shots) {
    const state = shotImageState(shot, linkedOf(shot));
    const wanted =
      state.kind === "empty" ||
      (state.kind === "node" && (state.phase === "idle" || state.phase === "failed"));
    if (!wanted) continue;
    if (shot.scene.trim()) targets.push(shot.id);
    else missingScene += 1;
  }
  return { targets, missingScene };
}

/**
 * 分镜表里能放大看的分镜图，按镜号排，没图的行跳过；项 id 用行 id，标题是「镜头 NN」。
 * @param srcs 和 shots 一一对应的图片地址（关联节点出的图或旧数据里手传的），没图为 null
 */
export function shotPreviewItems(
  shots: Pick<StoryboardShot, "id">[],
  srcs: (string | null)[],
): PreviewItem[] {
  return shots.flatMap((shot, i) => {
    const src = srcs[i];
    return src
      ? [{ id: shot.id, src, mediaType: "image", label: `镜头 ${formatShotNumber(i + 1)}` }]
      : [];
  });
}
