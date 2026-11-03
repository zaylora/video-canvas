import { useState } from "react";
import { ChevronDown } from "lucide-react";

/** 审批卡里条目多时默认露出的项数，其余收进「还有 N 项」 */
export const FOLD_LIMIT = 3;

/**
 * 折叠状态：超过 FOLD_LIMIT 项时只露前 FOLD_LIMIT 项，点「还有 N 项」后全部展开。
 * 折叠只影响显示，不影响勾选和批准的范围。
 * @param total 条目总数
 * @param limit 默认露出的项数，缺省 FOLD_LIMIT（展示卡片的表格用更大的值）
 */
export function useFold(total: number, limit: number = FOLD_LIMIT) {
  const [open, setOpen] = useState(false);
  const visible = open ? total : Math.min(total, limit);
  return { visible, hidden: total - visible, expand: () => setOpen(true) };
}

/** 「还有 N 项」按钮：点开后展开全部条目 */
export function MoreButton({ count, onClick }: { count: number; onClick: () => void }) {
  if (count <= 0) return null;
  return (
    <button
      type="button"
      onClick={onClick}
      className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 inline-flex h-6.5 items-center gap-1 self-start rounded-full px-2.5 text-xs outline-none transition-colors duration-120 focus-visible:ring-2"
    >
      <ChevronDown aria-hidden className="size-3.5" />
      还有 {count} 项
    </button>
  );
}
