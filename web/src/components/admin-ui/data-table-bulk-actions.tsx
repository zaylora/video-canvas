import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { X } from "lucide-react";
import { AnimatePresence, MotionConfig, motion } from "motion/react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 选中行后浮在页面底部的批量操作条（shadcn-admin 的 DataTableBulkActions）。
 * 方向键在按钮间移动，Esc 清除选择；选中数量变化时用 aria-live 播报。
 * 出现时从底部升起（y 16px → 0 + 淡入，DURATION.base），消失时反向（DURATION.exit）；
 * 开了「减少动态效果」只保留淡入淡出。count 变成 0 时组件自己播退出动画，所以调用方不要再按选中数卸载它。
 */
function DataTableBulkActions({
  count,
  entityName,
  onClear,
  children,
}: {
  count: number;
  /** 对象名，如“模型” */
  entityName: string;
  onClear: () => void;
  children: ReactNode;
}) {
  const toolbarRef = useRef<HTMLDivElement>(null);
  const [announcement, setAnnouncement] = useState("");

  useEffect(() => {
    if (count === 0) return;
    queueMicrotask(() => setAnnouncement(`已选 ${count} 个${entityName}，批量操作条可用`));
    const timer = setTimeout(() => setAnnouncement(""), 3000);
    return () => clearTimeout(timer);
  }, [count, entityName]);

  const onKeyDown = (event: KeyboardEvent) => {
    const buttons = toolbarRef.current?.querySelectorAll("button");
    if (!buttons?.length) return;
    const list = Array.from(buttons);
    const index = list.findIndex((button) => button === document.activeElement);
    if (event.key === "ArrowRight") {
      event.preventDefault();
      list[(index + 1) % list.length]?.focus();
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      list[index <= 0 ? list.length - 1 : index - 1]?.focus();
    } else if (event.key === "Home") {
      event.preventDefault();
      list[0]?.focus();
    } else if (event.key === "End") {
      event.preventDefault();
      list[list.length - 1]?.focus();
    } else if (event.key === "Escape") {
      // 菜单 / 弹窗里按 Esc 是关它们，不清除选择
      const target = event.target as HTMLElement;
      if (target.closest('[role="menu"], [role="dialog"], [data-slot="dropdown-menu-content"]'))
        return;
      event.preventDefault();
      onClear();
    }
  };

  return (
    <MotionConfig reducedMotion="user">
      <div aria-live="polite" aria-atomic="true" className="sr-only" role="status">
        {announcement}
      </div>
      <AnimatePresence>
        {count > 0 && (
          <motion.div
            key="bulk-actions"
            ref={toolbarRef}
            role="toolbar"
            aria-label={`已选 ${count} 个${entityName}的批量操作`}
            tabIndex={-1}
            onKeyDown={onKeyDown}
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0, transition: { duration: DURATION.base, ease: EASE_OUT } }}
            exit={{ opacity: 0, y: 16, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
            className={cn(
              "fixed bottom-6 left-1/2 z-40 -translate-x-1/2 rounded-xl",
              "focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none",
            )}
          >
            <div className="bg-background/95 supports-backdrop-filter:bg-background/60 flex items-center gap-x-2 rounded-xl border p-2 shadow-xl backdrop-blur-lg">
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="outline"
                      size="icon"
                      onClick={onClear}
                      className="size-6 rounded-full"
                      aria-label="清除选择"
                    />
                  }
                >
                  <X />
                </TooltipTrigger>
                <TooltipContent>清除选择（Esc）</TooltipContent>
              </Tooltip>
              <Separator
                orientation="vertical"
                className="data-[orientation=vertical]:h-5"
                aria-hidden="true"
              />
              <div className="flex items-center gap-x-1 text-sm">
                <Badge className="min-w-8 rounded-lg" aria-label={`已选 ${count} 个`}>
                  {count}
                </Badge>
                <span className="hidden sm:inline">个{entityName}</span> 已选
              </div>
              <Separator
                orientation="vertical"
                className="data-[orientation=vertical]:h-5"
                aria-hidden="true"
              />
              {children}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </MotionConfig>
  );
}

/** 批量操作条里的图标按钮：带提示文字 */
function BulkActionButton({
  label,
  icon,
  onClick,
  disabled,
  variant = "outline",
}: {
  label: string;
  icon: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  variant?: "outline" | "default" | "destructive";
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant={variant}
            size="icon"
            className="size-8"
            aria-label={label}
            disabled={disabled}
            onClick={onClick}
          />
        }
      >
        {icon}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

export { DataTableBulkActions, BulkActionButton };
