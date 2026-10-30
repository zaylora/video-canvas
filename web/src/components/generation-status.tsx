import { useEffect, type ComponentProps } from "react";

import { FailedPlaceholder, PendingPlaceholder } from "@/components/generation-placeholder";
import { useNow } from "@/hooks/use-now";
import { useTask } from "@/store/tasks";
import {
  deriveTaskStatus,
  type GenerationStatusView,
  type TaskStatusFallback,
} from "@/utils/tasks/node-view";
import { pendingCopy } from "@/utils/tasks/pending-copy";
import { isTerminalStatus } from "@/utils/tasks/status";
import { reconcileTaskIds } from "@/utils/ws/task-events";

/** 通用状态展示：由调用方决定框尺寸及操作，不包含页面布局或成功产物。 */
type GenerationStatusProps = {
  /** 已推导的排队、生成、保存或失败状态 */
  view: GenerationStatusView;
  /** 产物种类，决定保存阶段的文案 */
  mediaType?: "image" | "video" | "audio";
} & Pick<
  ComponentProps<typeof PendingPlaceholder>,
  "className" | "style" | "onCancel" | "cancelling"
> &
  Pick<ComponentProps<typeof FailedPlaceholder>, "onRetry" | "retryDisabled" | "retryHint">;

/** 画布、首页和片场共用的状态展示；转存阶段不再提供取消操作。 */
export function GenerationStatus({
  view,
  mediaType = "video",
  className,
  style,
  onCancel,
  cancelling,
  onRetry,
  retryDisabled,
  retryHint,
}: GenerationStatusProps) {
  if (view.phase === "failed") {
    return (
      <FailedPlaceholder
        message={view.message}
        refunded={view.refunded}
        taskRef={view.taskRef}
        onRetry={onRetry}
        retryDisabled={retryDisabled}
        retryHint={retryHint}
        className={className}
        style={style}
      />
    );
  }
  return (
    <PendingPlaceholder
      {...pendingCopy(view, mediaType)}
      onCancel={view.phase === "finalizing" ? undefined : onCancel}
      cancelling={cancelling}
      className={className}
      style={style}
    />
  );
}

/** 接收任务 ID 的状态组件；已有页面摘要时可在完整快照到达前展示失败原因。 */
type TaskGenerationStatusProps = Omit<GenerationStatusProps, "view"> & {
  /** 尚未获得任务 ID 时显示同步状态 */
  taskId?: string;
  /** 已有的任务摘要；不根据摘要的创建时间猜测实际生成耗时 */
  fallback?: TaskStatusFallback;
};

/** 统一订阅任务库、补查缺失快照并刷新耗时；后续进度沿用 WebSocket 和全局对账。 */
export function TaskGenerationStatus({ taskId, fallback, ...props }: TaskGenerationStatusProps) {
  const task = useTask(taskId);
  const status =
    fallback && isTerminalStatus(fallback.status)
      ? fallback.status
      : (task?.status ?? fallback?.status);
  const now = useNow(!status || !isTerminalStatus(status));
  const needsSync =
    !task ||
    Boolean(fallback && isTerminalStatus(fallback.status) && !isTerminalStatus(task.status));
  useEffect(() => {
    if (taskId && needsSync) void reconcileTaskIds([taskId]);
  }, [taskId, needsSync]);
  return <GenerationStatus {...props} view={deriveTaskStatus(task, now, fallback)} />;
}
