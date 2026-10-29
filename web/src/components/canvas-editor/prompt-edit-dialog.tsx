import type { ReactNode } from "react";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { usePromptEditor } from "@/store/prompt-editor";

/** 弹框里生成面板的宽度：和画布里节点下方的面板差不多宽 */
const PANEL_WIDTH = 560;

/**
 * 在弹框里编辑节点的生成设置（提示词、模型、参数、参考），给编辑器之外的页面用（短剧资产列表的卡片）。
 * 挂在节点组件里：生成面板的状态都在节点的 hook 上，弹框直接复用同一份，不另起一套。
 * 节点订阅 usePromptEditor 看是不是在叫自己。
 */
export function PromptEditDialog({
  id,
  title,
  actions,
  children,
}: {
  id: string;
  title: string;
  /** 标题行右侧的附加按钮（角色卡放「角色详情」） */
  actions?: ReactNode;
  /** 生成面板，拿到弹框给的宽度 */
  children: (width: number) => ReactNode;
}) {
  const open = usePromptEditor((state) => state.nodeId === id);
  return (
    <Dialog open={open} onOpenChange={(next) => !next && usePromptEditor.getState().close()}>
      {open && (
        <DialogContent
          // 弹框渲染在节点组件里，React 事件会沿组件树冒泡到节点和画布：
          // 拦下单击（免得在列表后面把节点选中，Delete 误删）和双击（免得被当成双击空白画布）
          onClick={(event) => event.stopPropagation()}
          onDoubleClick={(event) => event.stopPropagation()}
          className="w-auto gap-2 p-2 pt-4 sm:max-w-none"
        >
          <DialogHeader className="flex-row items-center gap-2 pr-10 pl-4">
            <DialogTitle className="min-w-0 flex-1 truncate">{title}</DialogTitle>
            <DialogDescription className="sr-only">编辑提示词、模型和参数后生成</DialogDescription>
            {actions}
          </DialogHeader>
          {/* 窄屏收窄到不超出屏幕 */}
          {children(Math.min(PANEL_WIDTH, window.innerWidth - 48))}
        </DialogContent>
      )}
    </Dialog>
  );
}

/** 弹框里的面板不再需要浮层自己的底、描边和阴影 */
export const PROMPT_DIALOG_PANEL_CLASS = "bg-transparent shadow-none ring-0";
