import { Fragment, type ReactElement, type ReactNode } from "react";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";

/** 右键菜单里的一项 */
export type ContextAction = {
  /** 唯一键 */
  key: string;
  label: ReactNode;
  icon?: ReactNode;
  onSelect: () => void;
  /** 危险操作（删除等）：标红 */
  destructive?: boolean;
  /** 不可用时给出原因（作为悬停提示），并置灰；没有原因就是可用 */
  disabledReason?: string | null;
  /** 在这一项前面画一条分隔线 */
  separated?: boolean;
};

/**
 * 通用右键菜单：包住要响应右键的元素，传一组操作即可。
 * 外观是半透明胶囊风格（规范 web/docs/coding-standards.md 6.1）。
 * 没有可显示的操作时不包菜单，直接渲染子元素。
 * @param actions 菜单项，按顺序显示
 * @param render 让触发区直接用这个元素（如按钮），不额外包一层 div；不给时包一层撑满宽度的 div
 */
export function ActionContextMenu({
  actions,
  render,
  className,
  children,
}: {
  actions: readonly ContextAction[];
  render?: ReactElement;
  className?: string;
  children?: ReactNode;
}) {
  if (actions.length === 0) return render ?? <>{children}</>;
  return (
    <ContextMenu>
      <ContextMenuTrigger render={render} className={cn(!render && "block w-full", className)}>
        {children}
      </ContextMenuTrigger>
      <ContextMenuContent className="bg-chrome ring-chrome-border min-w-40 rounded-xl p-1.5 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150">
        {actions.map((action) => (
          <Fragment key={action.key}>
            {action.separated && <ContextMenuSeparator className="bg-chrome-border mx-0.5" />}
            <ContextMenuItem
              variant={action.destructive ? "destructive" : "default"}
              disabled={!!action.disabledReason}
              title={action.disabledReason ?? undefined}
              onClick={action.onSelect}
              className="focus:bg-chrome-hover h-8 rounded-lg px-2.5"
            >
              {action.icon}
              {action.label}
            </ContextMenuItem>
          </Fragment>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
