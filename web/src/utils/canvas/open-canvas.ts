import type { CanvasDetailDto } from "@/api/canvas/type";

import { reconcileDraft, type Recovery } from "./draft-reconcile";
import type { Draft } from "./draft-store";

export type OpenedCanvas = {
  canvas: CanvasDetailDto;
  /** 用本地草稿恢复了、或草稿和云端冲突时有值；界面据此提示或弹冲突框 */
  recovery: Recovery | null;
};

/**
 * 打开画布：读云端，再和本地草稿对账。
 * - 恢复：用草稿内容打开，版本仍是云端版本，保存时带这个版本号作基准
 * - 冲突：打开云端最新版本，草稿内容交给冲突弹窗，让用户选
 * 云端读取失败照常抛出；草稿读取失败按没有草稿处理（draftStore 自身不抛错）。
 */
export async function loadCanvasForEditing({
  canvasId,
  userId,
  getCanvas,
  loadDraft,
  removeDraft,
}: {
  canvasId: string;
  userId: string | null;
  getCanvas: (id: string) => Promise<CanvasDetailDto>;
  loadDraft: (userId: string, canvasId: string) => Promise<Draft | null>;
  removeDraft: (canvasId: string) => Promise<void>;
}): Promise<OpenedCanvas> {
  const canvas = await getCanvas(canvasId);
  if (!userId) return { canvas, recovery: null };

  const result = reconcileDraft({
    draft: await loadDraft(userId, canvasId),
    cloudVersion: canvas.version,
    cloudGraph: canvas.graph,
  });
  switch (result.kind) {
    case "restore":
      return { canvas: { ...canvas, graph: result.graph }, recovery: { kind: "restored" } };
    case "conflict":
      return { canvas, recovery: { kind: "conflict", graph: result.graph } };
    case "discard":
      await removeDraft(canvasId);
      return { canvas, recovery: null };
    default:
      return { canvas, recovery: null };
  }
}
