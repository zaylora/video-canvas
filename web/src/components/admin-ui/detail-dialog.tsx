import type { ComponentProps, ReactNode } from "react";
import { X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/** 弹窗宽高：md 用于插件、渠道这类中等详情，lg 用于技能这类带文件树的大详情 */
const SIZE_CLASS = {
  md: "h-[min(88svh,760px)] sm:max-w-3xl",
  lg: "h-[min(90svh,880px)] sm:max-w-5xl",
} as const;

/**
 * 后台“详情弹窗”的外壳：固定高度、无内边距、纵向排布（头部 / 页签 / 内容区）。
 * 约定：点击那一刻就把 `open` 置真，头部用列表里已有的信息，内容区在接口返回前显示骨架（见 DetailDialogBody）。
 * @param open 是否打开；不要等接口返回再置真
 * @param onClose 点遮罩、按 Esc 或头部关闭按钮时调用
 * @param size 弹窗尺寸
 */
function DetailDialog({
  open,
  onClose,
  size = "md",
  className,
  children,
}: {
  open: boolean;
  onClose: () => void;
  size?: keyof typeof SIZE_CLASS;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "flex max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0",
          SIZE_CLASS[size],
          className,
        )}
      >
        {children}
      </DialogContent>
    </Dialog>
  );
}

/**
 * 详情弹窗头部：头像、标题、徽标、副标题、右侧操作与关闭按钮。
 * 标题为空（列表里也没有这一行，比如深链进来）时显示占位条，屏幕阅读器仍读到 `fallbackTitle`。
 * @param avatar 头像节点
 * @param title 标题；null 表示还没有
 * @param badges 标题后面的徽标
 * @param description 标题下面的一行小字
 * @param actions 关闭按钮左边的操作（开关、菜单等）
 * @param fallbackTitle 标题还没有时给读屏用的名字
 */
function DetailDialogHeader({
  avatar,
  title,
  badges,
  description,
  actions,
  fallbackTitle,
  onClose,
}: {
  avatar?: ReactNode;
  title: ReactNode | null;
  badges?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  fallbackTitle: string;
  onClose: () => void;
}) {
  const ready = title !== null;
  return (
    <div className="flex items-start gap-4 border-b px-6 py-4">
      {ready ? avatar : <Skeleton className="size-10 shrink-0 rounded-xl" />}
      <div className="min-w-0 flex-1">
        {ready ? (
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle className="text-lg font-semibold">{title}</DialogTitle>
            {badges}
          </div>
        ) : (
          <>
            <DialogTitle className="sr-only">{fallbackTitle}</DialogTitle>
            <Skeleton className="h-6 w-48" />
          </>
        )}
        {ready ? (
          <DialogDescription className="text-muted-foreground mt-0.5 truncate text-sm">
            {description}
          </DialogDescription>
        ) : (
          <>
            <DialogDescription className="sr-only">加载中</DialogDescription>
            <Skeleton className="mt-1.5 h-4 w-32" />
          </>
        )}
      </div>
      {actions}
      <Button type="button" variant="ghost" size="icon-sm" aria-label="关闭" onClick={onClose}>
        <X />
      </Button>
    </div>
  );
}

/** 默认的内容骨架：一段说明加两块卡片，不知道内容长什么样时用它 */
function DetailDialogSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-5 w-2/3" />
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-40 w-full" />
    </div>
  );
}

/**
 * 详情弹窗的内容区（可滚动）。`loading` 时显示骨架，接口返回后换成 children。
 * @param loading 接口还没返回
 * @param skeleton 自定义骨架，默认 DetailDialogSkeleton；最好和真实内容的版式接近，避免换上内容时跳动
 */
function DetailDialogBody({
  loading,
  skeleton,
  className,
  children,
  ...props
}: { loading: boolean; skeleton?: ReactNode } & ComponentProps<"div">) {
  return (
    <div
      data-slot="detail-dialog-body"
      aria-busy={loading || undefined}
      className={cn("min-h-0 flex-1 overflow-y-auto px-6 py-5", className)}
      {...props}
    >
      {loading ? (skeleton ?? <DetailDialogSkeleton />) : children}
    </div>
  );
}

export { DetailDialog, DetailDialogBody, DetailDialogHeader, DetailDialogSkeleton };
