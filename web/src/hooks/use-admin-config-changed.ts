import { useEffect, useRef } from "react";

import {
  ADMIN_CONFIG_CHANGED,
  type AdminConfigChangedDetail,
  type AdminConfigTarget,
} from "@/utils/admin/config-changed";

/**
 * 后台助手应用了变更单后，重新拉数据：监听 window 的 admin-config-changed，
 * 只有这次改动涉及 target 时才调用 onChange
 * @param target 本页关心的配置对象
 * @param onChange 重新拉数据的回调
 */
export function useAdminConfigChanged(target: AdminConfigTarget, onChange: () => void): void {
  const latest = useRef(onChange);
  useEffect(() => {
    latest.current = onChange;
  });
  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<AdminConfigChangedDetail>).detail;
      if (detail?.targets?.includes(target)) latest.current();
    };
    window.addEventListener(ADMIN_CONFIG_CHANGED, listener);
    return () => window.removeEventListener(ADMIN_CONFIG_CHANGED, listener);
  }, [target]);
}
