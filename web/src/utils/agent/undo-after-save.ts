/** 撤销本轮的结果：成功时带服务端返回的内容，失败时说明是哪一步没成 */
export type UndoAfterSaveResult<T> =
  | { ok: true; value: T }
  | { ok: false; reason: "save-failed" | "undo-failed" };

/**
 * 先把本地还没保存的改动存上，再请求服务端撤销本轮。
 *
 * 服务端判断「你有没有改过 Agent 新建的节点」，靠的是它看到的节点和 Agent 写入时是否一样。
 * 你在防抖窗口内刚改的提示词、刚连的线，服务端还不知道，直接撤销就会把它们连同节点一起删掉。
 * 保存没成功（离线、冲突）时不撤销：宁可让你重试，也不删掉你没存上的改动。
 * @param deps.save 立即保存，返回保存后云端是否没有未保存内容
 * @param deps.undo 请求撤销，失败返回 null
 */
export async function undoAfterSave<T>(deps: {
  save: () => Promise<boolean>;
  undo: () => Promise<T | null>;
}): Promise<UndoAfterSaveResult<T>> {
  let saved = false;
  try {
    saved = await deps.save();
  } catch {
    saved = false;
  }
  if (!saved) return { ok: false, reason: "save-failed" };
  const value = await deps.undo();
  return value === null ? { ok: false, reason: "undo-failed" } : { ok: true, value };
}
