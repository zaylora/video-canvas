import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { getStorageHybrid, setStorageHybrid } from "@/api/admin/storage";
import type { StorageHybridStatus } from "@/api/admin/storage/type.d";

import { useAliveRef } from "../use-admin";

/** 启用任务由后端持续执行，刷新页面后仍可继续观察结果。 */
export function useStorageHybrid(onChanged: () => void) {
  const alive = useAliveRef();
  const sequence = useRef({ last: 0 });
  const previous = useRef<StorageHybridStatus | null>(null);
  const [status, setStatus] = useState<StorageHybridStatus | null>(null);
  const [saving, setSaving] = useState(false);

  const accept = useCallback(
    (next: StorageHybridStatus, request: number) => {
      if (request !== sequence.current.last) return;
      const was = previous.current?.state;
      previous.current = next;
      setStatus(next);
      if (was === "preparing" && next.state === "active") {
        toast.success("混合策略已开启");
        onChanged();
      } else if (was === "preparing" && next.state === "failed") {
        toast.error(next.error || "混合策略开启失败");
        onChanged();
      }
    },
    [onChanged],
  );

  const refresh = useCallback(async () => {
    const request = ++sequence.current.last;
    try {
      accept(await getStorageHybrid(), request);
    } catch {
      /** 保留最近一次成功状态，网络恢复后继续刷新。 */
    }
  }, [accept]);

  useEffect(() => {
    void refresh();
    const order = sequence.current;
    return () => {
      order.last++;
    };
  }, [refresh]);

  useEffect(() => {
    if (saving) return;
    const interval = window.setInterval(
      () => void refresh(),
      status?.state === "preparing" ? 1500 : 15_000,
    );
    return () => window.clearInterval(interval);
  }, [refresh, saving, status?.state]);

  const change = useCallback(
    async (enabled: boolean, limitBytes = 0) => {
      const request = ++sequence.current.last;
      setSaving(true);
      try {
        const next = await setStorageHybrid({ enabled, limit_bytes: limitBytes });
        accept(next, request);
        if (!enabled && next.state === "off") {
          toast.success("混合策略已关闭");
          onChanged();
        }
      } catch {
        /** 请求错误由拦截器展示。再读状态，覆盖响应丢失但操作已入库的情况。 */
        await refresh();
      } finally {
        if (alive.current) setSaving(false);
      }
    },
    [accept, alive, onChanged, refresh],
  );

  return { status, saving, change, refresh };
}
