import { useState } from "react";

import { setModelEnabled } from "@/api/admin/ai";
import type { ConfigListItem } from "@/api/admin/ai/type.d";

import { openDeleteDialog } from "../delete-dialog";

/**
 * 模型表格行上的动作（模型页与渠道页共用）：上线 / 下线开关、删除。
 * 删除的确认框走全局弹窗 store，页面不用自己挂对话框。
 * @param reload 操作成功后刷新列表
 */
export function useModelRowActions<T extends ConfigListItem = ConfigListItem>(
  reload: () => Promise<unknown> | void,
  /** 乐观更新：点开关立刻改列表里的行，失败再改回去 */
  patchItems?: (fn: (items: T[]) => T[]) => void,
) {
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const setLocal = (key: string, enabled: boolean) =>
    patchItems?.((items) => items.map((i) => (i.key === key ? { ...i, enabled } : i)));

  const toggleEnabled = async (key: string, enabled: boolean) => {
    setBusyKey(key);
    setLocal(key, enabled);
    try {
      await setModelEnabled(key, enabled);
      await reload();
    } catch {
      // 失败才回退开关；错误提示由全局拦截器弹
      setLocal(key, !enabled);
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
