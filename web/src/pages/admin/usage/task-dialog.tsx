import { SearchX } from "lucide-react";

import {
  DetailDialog,
  DetailDialogBody,
  DetailDialogHeader,
} from "@/components/admin-ui/detail-dialog";
import {
  EmptyState,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { LoadFailed } from "@/components/admin-ui/load-failed";
import { Tag } from "@/components/admin-ui/tag";
import { Skeleton } from "@/components/ui/skeleton";
import { useNow } from "@/hooks/use-now";
import { agoText, drawerSections, isActiveStatus, kindName } from "@/utils/admin/log-rules";

import { TraceTimeline } from "../ai/trace/trace-timeline";
import {
  AdviceHint,
  CreditBlock,
  FailureDetail,
  OutputsBlock,
  ParamsBlock,
  PromptBlock,
  TaskInfoBlock,
  TimelineBlock,
  VerdictCard,
} from "./task-sections";
import { useTaskDetail } from "./use-task-detail";

/** 内容区骨架：版式照真实内容画（结论卡 + 几行字），换上内容时不跳 */
function TaskBodySkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy>
      <Skeleton className="h-16 rounded-lg" />
      {Array.from({ length: 3 }, (_, i) => (
        <Skeleton key={i} className="h-4" style={{ width: `${55 + ((i * 17) % 40)}%` }} />
      ))}
    </div>
  );
}

/**
 * 任务详情弹窗（居中，不写进 URL）：结论卡、怎么办提示、提示词、产物、积分、经过、参数、失败原因详情、
 * 任务信息和试跑追踪一律全部显示。点一行的那一刻就打开，头部先用列表里已有的信息，
 * 内容区在接口返回前显示骨架；失败是「加载失败 · 重试」且不关闭弹窗，任务不存在显示「找不到这个任务」。
 * @param taskRef 要看的任务编号；弹窗关闭后保留，等关闭动画播完
 * @param onFilterUser 点用户名：把该用户加入列表筛选并关闭弹窗
 */
function TaskDialog({
  open,
  taskRef,
  onClose,
  onFilterUser,
}: {
  open: boolean;
  taskRef: string | null;
  onClose: () => void;
  onFilterUser: (user: { id: number; username: string }) => void;
}) {
  const { phase, detail, retry } = useTaskDetail(open ? taskRef : null);
  const task = detail?.task;
  const now = useNow(!!task && isActiveStatus(task.status));
  const sections = drawerSections({ hasTrace: !!detail && detail.trace !== null });

  return (
    <DetailDialog open={open} onClose={onClose}>
      <DetailDialogHeader
        fallbackTitle={phase === "notfound" ? "找不到这个任务" : "任务详情"}
        title={
          detail && task ? (
            <>
              <button
                type="button"
                title="只看这个用户"
                className="hover:text-primary focus-visible:ring-ring/50 rounded-sm underline-offset-2 outline-none hover:underline focus-visible:ring-2"
                onClick={() => onFilterUser(detail.user)}
              >
                {detail.user.username}
              </button>
              {` · 生成${kindName(task.kind)}`}
            </>
          ) : phase === "notfound" ? (
            "找不到这个任务"
          ) : null
        }
        badges={task?.is_test && <Tag tone="violet">试跑</Tag>}
        description={
          task ? `${agoText(task.created_at, now)} · ${task.model_name || task.model_key}` : " "
        }
        onClose={onClose}
      />

      <DetailDialogBody
        loading={phase === "loading"}
        skeleton={<TaskBodySkeleton />}
        className="flex flex-col gap-6"
      >
        {phase === "error" && <LoadFailed onRetry={retry} />}
        {phase === "notfound" && (
          <EmptyState className="mt-2">
            <EmptyStateIcon>
              <SearchX />
            </EmptyStateIcon>
            <EmptyStateTitle>找不到这个任务</EmptyStateTitle>
            <EmptyStateDescription>编号错误，或任务已不存在。</EmptyStateDescription>
          </EmptyState>
        )}
        {phase === "ready" && detail && (
          <>
            <div className="flex flex-col gap-3">
              {sections.verdict && <VerdictCard detail={detail} now={now} />}
              {sections.advice && <AdviceHint detail={detail} />}
            </div>
            {sections.prompt && <PromptBlock input={detail.input} />}
            {sections.outputs && <OutputsBlock detail={detail} />}
            {sections.creditSentence && (
              <CreditBlock detail={detail} ledger={sections.creditLedger} />
            )}
            {sections.timeline && <TimelineBlock detail={detail} />}
            {sections.params && <ParamsBlock input={detail.input} />}
            {sections.failureDetail && <FailureDetail detail={detail} />}
            {sections.taskInfo && <TaskInfoBlock detail={detail} />}
            {sections.trace && detail.trace !== null && (
              <TraceTimeline
                taskId={detail.task.id}
                steps={detail.trace as Parameters<typeof TraceTimeline>[0]["steps"]}
              />
            )}
          </>
        )}
      </DetailDialogBody>
    </DetailDialog>
  );
}

export { TaskDialog };
