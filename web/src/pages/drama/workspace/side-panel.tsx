import { Bot } from "lucide-react";

import { cn } from "@/lib/utils";

import { AssistantPane } from "../assistant/assistant-pane";

/** 右栏：短剧助手（复用画布 Agent）。任务进度不放这里，由工作台顶栏下方的进度条显示 */
export function SidePanel({ className }: { className?: string }) {
  return (
    <aside
      aria-label="右栏"
      className={cn("border-border bg-background flex w-80 shrink-0 flex-col border-l", className)}
    >
      <div className="flex items-center gap-2 p-3 text-sm font-medium">
        <Bot className="size-4" />
        助手
      </div>
      <div className="min-h-0 flex-1">
        <AssistantPane />
      </div>
    </aside>
  );
}
