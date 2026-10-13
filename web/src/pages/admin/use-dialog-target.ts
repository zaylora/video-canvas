import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router";

import { readDialogEntry } from "@/utils/admin/dialog-entry";

/**
 * 后台页面里“弹窗开着什么”的状态：放在页面自己的 state 里，不进 URL，
 * 所以打开、关闭弹窗不会把列表的类型、搜索、页码筛选冲掉。
 * 别的页面要直接打开某个弹窗，用 `dialogEntry` 把目标放进路由 state：本页收到后打开弹窗，
 * 并立刻把路由 state 清掉，刷新或后退都不会再弹。
 * @returns [当前要打开的目标（null 为关闭）, 设置目标]
 */
export function useDialogTarget<T>(): readonly [T | null, (next: T | null) => void] {
  const location = useLocation();
  const navigate = useNavigate();
  const [target, setTarget] = useState<T | null>(null);
  /** 已经处理过的那次导航，同一次导航只打开一次 */
  const [handled, setHandled] = useState<string | null>(null);

  const entry = readDialogEntry<T>(location.state);
  if (entry !== null && handled !== location.key) {
    setHandled(location.key);
    setTarget(entry);
  }

  // 用过就清：replace 只换 state，不增加历史记录
  const hasEntry = entry !== null;
  useEffect(() => {
    if (hasEntry)
      void navigate({ pathname: location.pathname, search: location.search }, { replace: true });
  }, [hasEntry, location.key, location.pathname, location.search, navigate]);

  return [target, setTarget];
}
