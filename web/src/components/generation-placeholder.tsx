import type { CSSProperties } from "react";
import { Loader2, RotateCcw, TriangleAlert, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { TaskIdTag } from "@/components/task-id-tag";
import { cn } from "@/lib/utils";

type PlaceholderBoxProps = {
  /** 框的尺寸、圆角等：画布节点按画幅给 style，首页格子给高度类，这里不定尺寸 */
  className?: string;
  style?: CSSProperties;
};

/**
 * 生成中的占位：灰色骨架上叠状态文字（转圈 + 标题 + 进度），下面一行说明，进度已知时再画进度条。
 * 画布节点和首页结果格子共用，纯展示，任务状态怎么来的、取消怎么接都由调用方决定。
 * 在画布节点里，调用方要给 `nodrag`（取消按钮已带）并让外层负责圆角。
 * @param title 标题，如「生成中」
 * @param detail 标题下的小字说明
 * @param progress 进度 0–100；未知不传或传 null
 * @param onCancel 取消任务；不传就不显示按钮
 * @param cancelling 取消请求进行中：按钮禁用并转圈
 */
export function PendingPlaceholder({
  title,
  detail,
  progress,
  onCancel,
  cancelling,
  className,
  style,
}: PlaceholderBoxProps & {
  title: string;
  detail?: string;
  progress?: number | null;
  onCancel?: () => void;
  cancelling?: boolean;
}) {
  const hasProgress = typeof progress === "number";
  return (
    <div
      data-slot="generation-placeholder"
      className={cn("relative overflow-hidden", className)}
      style={style}
      role="status"
      aria-live="polite"
    >
      <Skeleton className="absolute inset-0 rounded-none" />
      <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center">
        <div className="text-foreground/80 flex items-center gap-2 text-sm font-medium">
          <Loader2 className="size-4 animate-spin" />
          {title}
          {hasProgress && <span className="tabular-nums">{progress}%</span>}
        </div>
        {detail && <p className="text-muted-foreground text-xs tabular-nums">{detail}</p>}
        {hasProgress && (
          <div className="bg-foreground/10 h-1 w-32 overflow-hidden rounded-full">
            <div
              className="bg-foreground/60 h-full rounded-full transition-[width] duration-500"
              style={{ width: `${progress}%` }}
            />
          </div>
        )}
      </div>
      {onCancel && (
        <Button
          variant="outline"
          size="xs"
          className="nodrag bg-card/80 absolute right-2 bottom-2 backdrop-blur"
          disabled={cancelling}
          onClick={onCancel}
        >
          {cancelling ? <Loader2 className="animate-spin" /> : <X />}
          取消
        </Button>
      )}
    </div>
  );
}

/**
 * 生成失败的占位：失败原因、退款说明、可复制的任务编号和重试按钮。
 * 画布节点和首页结果格子共用，纯展示。
 * @param message 给用户看的失败原因
 * @param refunded 是否提示「积分已退回」
 * @param taskRef 任务编号，到后端日志里定位用；没有就不显示
 * @param onRetry 重新提交一个新任务；不传就不显示按钮
 * @param retryDisabled 重试按钮是否禁用
 * @param retryHint 重试按钮悬停时的说明，写清为什么点不了
 */
export function FailedPlaceholder({
  message,
  refunded,
  taskRef,
  onRetry,
  retryDisabled,
  retryHint,
  className,
  style,
}: PlaceholderBoxProps & {
  message: string;
  refunded?: boolean;
  taskRef?: string | null;
  onRetry?: () => void;
  retryDisabled?: boolean;
  retryHint?: string;
}) {
  return (
    <div
      data-slot="generation-placeholder"
      className={cn(
        "bg-destructive/5 text-destructive flex flex-col items-center justify-center gap-2 px-4 text-center text-xs",
        className,
      )}
      style={style}
      role="alert"
    >
      <TriangleAlert className="size-4 shrink-0" />
      <p className="line-clamp-3 break-words">{message}</p>
      {refunded && <p className="text-muted-foreground">积分已退回</p>}
      <TaskIdTag id={taskRef} className="nodrag nowheel" />
      {onRetry && (
        <span title={retryHint}>
          <Button
            variant="outline"
            size="xs"
            className="nodrag"
            disabled={retryDisabled}
            onClick={onRetry}
          >
            <RotateCcw />
            重试
          </Button>
        </span>
      )}
    </div>
  );
}
