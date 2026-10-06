import { useEffect, useState } from "react";

import { canvasLockName, watchCanvasLock, type LockLike } from "@/utils/canvas/canvas-lock";
import { getCurrentUserId } from "@/utils/storage/user-id";

/** checking：还没问出结果；held：可以编辑；waiting：已在另一个标签页打开 */
export type CanvasLockStatus = "checking" | "held" | "waiting";

/**
 * 同一张画布只允许一个标签页编辑。认不出当前用户时没有草稿，不需要锁，直接放行。
 * 状态按锁名记录：换画布时旧结果自动失效，回到 checking，不需要在 effect 里同步重置。
 */
export function useCanvasLock(canvasId: string | undefined): CanvasLockStatus {
  const userId = getCurrentUserId();
  const name = userId && canvasId ? canvasLockName(userId, canvasId) : null;
  const [result, setResult] = useState<{ name: string; state: "held" | "waiting" } | null>(null);

  useEffect(() => {
    if (!name) return;
    const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
    return watchCanvasLock({
      name,
      locks: locks as unknown as LockLike | undefined,
      onChange: (state) => setResult({ name, state }),
    });
  }, [name]);

  if (!name) return "held";
  return result?.name === name ? result.state : "checking";
}
