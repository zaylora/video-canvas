import { useState } from "react";
import { toast } from "sonner";

import { rollbackModel, setModelEnabled } from "@/api/admin-ai";
import type { ConfigListItem, ConfigRevision } from "@/api/admin-ai/type";
import { publishBlockReason, resolveModelChannel } from "@/utils/admin/model-channel";

import { openDeleteDialog } from "../delete-dialog";
import type { AdminCatalog } from "../use-admin";
import { confirmRollback } from "./publish-dialog";

/**
 * 模型表格行上的动作（模型页与渠道页共用）：上线 / 下线开关、从版本历史回滚、删除。
 * 回滚和删除的确认框都走全局弹窗 store，页面不用自己挂对话框。
 * @param reload 操作成功后刷新列表
 */
export function useModelRowActions(catalog: AdminCatalog, reload: () => Promise<unknown> | void) {
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const toggleEnabled = async (key: string, enabled: boolean) => {
    setBusyKey(key);
    try {
      await setModelEnabled(key, enabled);
      toast.success(`${key} 已${enabled ? "上线" : "下线"}`, {
        description: enabled ? "画布里立即可见。" : "画布里不再显示，已发起的任务不受影响。",
        action: { label: "撤销", onClick: () => void toggleEnabled(key, !enabled) },
      });
      await reload();
    } finally {
      setBusyKey(null);
    }
  };

  const requestRollback = (key: string, revision: ConfigRevision) => {
    const info = resolveModelChannel(revision.body_json, catalog.channels, catalog.plugins);
    void confirmRollback({
      modelKey: key,
      revision,
      info,
      blockReason: publishBlockReason(info, catalog.channelsStatus === "ready"),
      onConfirm: async () => {
        await rollbackModel(key, revision.id);
        toast.success(`已回滚到 v${revision.revision_no}`);
        await reload();
      },
    });
  };

  const requestDelete = (item: ConfigListItem) =>
    openDeleteDialog({
      target: "model",
      objectKey: item.key,
      name: item.label || item.name || item.key,
      onDeleted: () => void reload(),
    });

  return { busyKey, toggleEnabled, requestRollback, requestDelete };
}
