import { useState } from "react";
import { toast } from "sonner";

import { rollbackModel, setModelEnabled } from "@/api/admin-ai";
import type { ConfigRevision } from "@/api/admin-ai/type";
import { errorMessage } from "@/utils/admin/errors";
import { publishBlockReason, resolveModelChannel } from "@/utils/admin/model-channel";

import type { AdminCatalog } from "../use-admin";

/**
 * 模型表格行上的动作（模型页与渠道页共用）：上架开关、从版本历史回滚。
 * 回滚走二次确认：返回的 rollback 直接展开给 RollbackDialog。
 * @param reload 操作成功后刷新列表
 */
export function useModelRowActions(catalog: AdminCatalog, reload: () => Promise<unknown> | void) {
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [target, setTarget] = useState<{ key: string; revision: ConfigRevision } | null>(null);
  const [rollbackBusy, setRollbackBusy] = useState(false);
  const [rollbackError, setRollbackError] = useState<string | null>(null);

  const toggleEnabled = async (key: string, enabled: boolean) => {
    setBusyKey(key);
    try {
      await setModelEnabled(key, enabled);
      toast.success(`${key} 已${enabled ? "上架" : "下架"}`, {
        description: enabled ? "画布里立即可见。" : "画布里不再显示，已发起的任务不受影响。",
      });
      await reload();
    } finally {
      setBusyKey(null);
    }
  };

  const info = resolveModelChannel(target?.revision.body_json, catalog.channels, catalog.plugins);

  return {
    busyKey,
    toggleEnabled,
    requestRollback: (key: string, revision: ConfigRevision) => {
      setRollbackError(null);
      setTarget({ key, revision });
    },
    rollback: {
      revision: target?.revision ?? null,
      modelKey: target?.key ?? "",
      info,
      blockReason: target ? publishBlockReason(info, catalog.channelsStatus === "ready") : null,
      busy: rollbackBusy,
      error: rollbackError,
      onCancel: () => setTarget(null),
      onConfirm: async () => {
        if (!target) return;
        setRollbackBusy(true);
        setRollbackError(null);
        try {
          await rollbackModel(target.key, target.revision.id);
          toast.success(`已回滚到 v${target.revision.revision_no}`);
          setTarget(null);
          await reload();
        } catch (error) {
          setRollbackError(errorMessage(error, "回滚失败"));
        } finally {
          setRollbackBusy(false);
        }
      },
    },
  };
}
