import { useCallback, useMemo, useState } from "react";
import { useLocation } from "react-router";

import type { AdminPageContext } from "@/api/agent/type";
import { useAdminPageHints } from "@/store/admin-page-hints";
import {
  buildPageContext,
  pageContextChips,
  type ContextChip,
  type ContextChipId,
} from "@/utils/admin/assistant/page-context";

/** 没有移除任何标签时共用的空集合 */
const NO_CHIPS: ReadonlySet<ContextChipId> = new Set();

/**
 * 后台助手输入框里的页面上下文：按当前路由和页面登记的选中项生成引用标签，用户可以移除；
 * 发送时 `context` 作为 page_context 带上。移除是记在页面上的：换了页面就不再沿用。
 */
export function useAdminPageContext(): {
  /** 输入框里显示的引用标签 */
  chips: ContextChip[];
  /** 发送时带上的页面上下文，标签全被移除时为 undefined */
  context: AdminPageContext | undefined;
  /** 移除一个标签 */
  remove: (id: string) => void;
} {
  const { pathname } = useLocation();
  const hints = useAdminPageHints((s) => s.hints);
  const [removed, setRemoved] = useState<{ path: string; ids: ReadonlySet<ContextChipId> }>({
    path: "",
    ids: NO_CHIPS,
  });
  const dismissed = removed.path === pathname ? removed.ids : NO_CHIPS;
  const chips = useMemo(
    () => pageContextChips(pathname, hints).filter((c) => !dismissed.has(c.id)),
    [pathname, hints, dismissed],
  );
  const context = useMemo(
    () => buildPageContext(pathname, hints, dismissed),
    [pathname, hints, dismissed],
  );
  const remove = useCallback(
    (id: string) =>
      setRemoved((prev) => ({
        path: pathname,
        ids: new Set(prev.path === pathname ? prev.ids : NO_CHIPS).add(id as ContextChipId),
      })),
    [pathname],
  );
  return { chips, context, remove };
}
