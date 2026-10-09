import { useState } from "react";
import { toast } from "sonner";

import { setModelEnabled } from "@/api/admin/ai";
import type { ConfigListItem } from "@/api/admin/ai/type.d";

import { openDeleteDialog } from "../delete-dialog";

/**
 * 模型表格行上的动作（模型页与渠道页共用）：上线 / 下线开关、删除。
 * 删除的确认框走全局弹窗 store，页面不用自己挂对话框。
 * @param reload 操作成功后刷新列表
 */
export function useModelRowActions(reload: () => Promise<unknown> | void) {
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

  const requestDelete = (item: ConfigListItem) =>
    openDeleteDialog({
      target: "model",
      objectKey: item.key,
      name: item.label || item.name || item.key,
      onDeleted: () => void reload(),
    });

  return { busyKey, toggleEnabled, requestDelete };
}
