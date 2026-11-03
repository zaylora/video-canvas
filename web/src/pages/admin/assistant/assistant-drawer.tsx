import { useState } from "react";

import { ASSISTANT_NAME } from "@/constants/agent";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";

import { AdminAssistantPanel } from "./admin-assistant-panel";

/** 面板宽度（像素） */
const PANEL_WIDTH = 460;

/**
 * 后台右侧的助手抽屉：460px 宽的浮起面板（--chrome 半透明底、描边、画布助手同款大圆角），展开时推挤正文，不遮盖；
 * 窄屏（小于 md）改为盖在页面上。开关记在本浏览器。第一次展开时才挂载面板（之后一直保留，关掉再打开还在原处）。
 * 放在后台外壳里，与正文并排
 */
export function AssistantDrawer() {
  const open = useAdminStore((s) => s.assistantOpen);
  /** 展开过一次就一直保留面板，关掉再打开还在原处 */
  const [mounted, setMounted] = useState(open);
  if (open && !mounted) setMounted(true);
  return (
    <aside
      aria-label={ASSISTANT_NAME}
      inert={!open}
      style={{ "--assistant-w": `${PANEL_WIDTH}px` } as React.CSSProperties}
      className={cn(
        "shrink-0 overflow-hidden md:transition-[width] md:duration-200 md:ease-out motion-reduce:transition-none",
        open
          ? "max-md:fixed max-md:inset-2 max-md:z-40 md:w-[calc(var(--assistant-w)+0.5rem)]"
          : "max-md:hidden md:w-0",
      )}
    >
      <div
        className={cn(
          "bg-chrome ring-chrome-border flex h-full flex-col overflow-hidden rounded-3xl shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150",
          "md:my-2 md:mr-2 md:h-[calc(100svh-1rem)] md:w-(--assistant-w)",
        )}
      >
        {mounted && <AdminAssistantPanel />}
      </div>
    </aside>
  );
}
