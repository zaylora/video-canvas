/**
 * 退出登录前清理本机的画布数据：
 * - 先尽力把云端同步完（有超时，不能卡住退出）
 * - 视口记录总是清掉
 * - 草稿只在云端都同步完后才清：没同步完的草稿是用户唯一的副本，
 *   按用户隔离存着，下次登录同一账号还能恢复，不会被别人看到
 * 清理本身失败不影响退出。
 */
export async function cleanupBeforeLogout({
  userId,
  flushAll,
  clearDrafts,
  clearViewports,
  wait = (ms) => new Promise<void>((resolve) => setTimeout(resolve, ms)),
  timeoutMs = 1500,
}: {
  userId: string | null;
  flushAll: () => Promise<boolean>;
  clearDrafts: (userId: string) => Promise<void>;
  clearViewports: (userId: string) => void;
  wait?: (ms: number) => Promise<void>;
  timeoutMs?: number;
}): Promise<{ draftsCleared: boolean }> {
  if (!userId) return { draftsCleared: false };
  const synced = await Promise.race([
    flushAll().catch(() => false),
    wait(timeoutMs).then(() => false),
  ]);
  try {
    clearViewports(userId);
  } catch {
    // 清不掉不影响退出
  }
  if (!synced) return { draftsCleared: false };
  try {
    await clearDrafts(userId);
    return { draftsCleared: true };
  } catch {
    return { draftsCleared: false };
  }
}
