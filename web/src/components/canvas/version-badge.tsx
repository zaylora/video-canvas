import { ChevronDown, Loader2 } from "lucide-react";

import { useNodeHistoryStore } from "@/store/node-history";
import type { VersionBadge as VersionBadgeInfo } from "@/utils/canvas/versions";

/**
 * 节点右上角的多版本徽标「3 ▾」：数字是历史版本总数，还有任务在跑时带转圈。
 * 点它打开「历史记录」弹窗，是节点历史的唯一入口（功能区的历史按钮已删除）。
 * 常驻、不随选中显隐；nodrag nopan 不让点击变成拖节点，也不触发节点选中和双击预览。
 */
export function VersionBadge({ nodeId, badge }: { nodeId: string; badge: VersionBadgeInfo }) {
  const open = useNodeHistoryStore((state) => state.open);
  return (
    <button
      type="button"
      aria-label={`历史记录，共 ${badge.count} 版${badge.generating ? "，还有任务在生成" : ""}`}
      onClick={(event) => {
        event.stopPropagation();
        open(nodeId);
      }}
      onDoubleClick={(event) => event.stopPropagation()}
      className="nodrag nopan absolute top-2.5 right-2.5 z-10 inline-flex h-7.5 cursor-pointer items-center gap-1 rounded-[10px] bg-black/75 px-2.5 text-sm text-white tabular-nums ring-1 ring-white/10 outline-none transition-colors hover:bg-black/90 focus-visible:ring-2 focus-visible:ring-white/70"
    >
      {badge.generating && <Loader2 className="size-3 animate-spin opacity-80" />}
      {badge.count}
      <ChevronDown className="size-3.5" />
    </button>
  );
}
