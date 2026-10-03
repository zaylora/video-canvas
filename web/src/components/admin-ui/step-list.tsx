import type { ComponentProps } from "react";
import { CircleCheck, CircleDashed, CircleX, Loader2, MinusCircle } from "lucide-react";

import { cn } from "@/lib/utils";

/** 步骤状态：进行中、通过、失败、按配置跳过、因前面失败没执行 */
type StepStatus = "running" | "ok" | "failed" | "skipped" | "notrun";

const STEP_ICON: Record<
  StepStatus,
  { icon: typeof CircleCheck; className: string; label: string }
> = {
  running: { icon: Loader2, className: "animate-spin text-muted-foreground", label: "进行中" },
  ok: { icon: CircleCheck, className: "text-emerald-600 dark:text-emerald-400", label: "通过" },
  failed: { icon: CircleX, className: "text-red-600 dark:text-red-400", label: "失败" },
  skipped: { icon: MinusCircle, className: "text-muted-foreground", label: "已跳过" },
  notrun: { icon: CircleDashed, className: "text-muted-foreground/60", label: "未执行" },
};

/**
 * 分步结果列表（测试连接、预检）：图标加文字表达状态，不只靠颜色。
 * <StepList><StepListItem status="failed"><StepListIcon status="failed" /><StepListName>写入探针</StepListName><StepListDuration>51ms</StepListDuration><StepListDetail>…</StepListDetail></StepListItem></StepList>
 */
function StepList({ className, ...props }: ComponentProps<"ol">) {
  return (
    <ol
      data-slot="step-list"
      className={cn("flex flex-col gap-1.5 text-sm", className)}
      {...props}
    />
  );
}

function StepListItem({
  className,
  status,
  ...props
}: ComponentProps<"li"> & { status: StepStatus }) {
  return (
    <li
      data-slot="step-list-item"
      data-status={status}
      className={cn(
        "flex flex-wrap items-center gap-x-2 gap-y-1",
        "data-[status=skipped]:text-muted-foreground data-[status=notrun]:text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}

/** 状态图标；读屏读出状态文字 */
function StepListIcon({ status, className }: { status: StepStatus; className?: string }) {
  const { icon: Icon, className: tone, label } = STEP_ICON[status];
  return (
    <span data-slot="step-list-icon" role="img" aria-label={label} className="inline-flex">
      <Icon className={cn("size-4 shrink-0", tone, className)} />
    </span>
  );
}

function StepListName({ className, ...props }: ComponentProps<"span">) {
  return <span data-slot="step-list-name" className={cn("min-w-0", className)} {...props} />;
}

function StepListDuration({ className, ...props }: ComponentProps<"span">) {
  return (
    <span
      data-slot="step-list-duration"
      className={cn("text-muted-foreground ml-auto font-mono text-xs tabular-nums", className)}
      {...props}
    />
  );
}

/** 步骤下方缩进展示的说明（失败原因、处理建议） */
function StepListDetail({ className, ...props }: ComponentProps<"div">) {
  return (
    <div data-slot="step-list-detail" className={cn("basis-full pl-6", className)} {...props} />
  );
}

export {
  StepList,
  StepListDetail,
  StepListDuration,
  StepListIcon,
  StepListItem,
  StepListName,
  type StepStatus,
};
