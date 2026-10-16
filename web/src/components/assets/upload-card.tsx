import { motion } from "motion/react";
import { AlertCircle, Loader2, RotateCcw, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SPRING } from "@/lib/motion";
import type { UploadTask } from "@/utils/assets/upload-queue";

/**
 * 资产库里一个上传中的卡片：文件名 + 进度条，可取消；失败显示原因与「重试」「移除」。
 * 进度条用 scaleX 做，只动 transform；传完字节后服务端还要处理（登记），这时显示「处理中」。
 * @param task 上传任务
 * @param onCancel 取消上传，或移除失败的任务
 * @param onRetry 重试失败的任务
 */
export function UploadCard({
  task,
  onCancel,
  onRetry,
}: {
  task: UploadTask;
  onCancel: () => void;
  onRetry: () => void;
}) {
  const failed = task.status === "failed";
  const queued = task.status === "queued";
  const processing = task.status === "registering";

  return (
    <div
      data-slot="upload-card"
      data-status={task.status}
      className="bg-muted/50 ring-border relative flex aspect-square min-w-0 flex-col items-center justify-center gap-2 rounded-xl px-3 text-center ring-1"
    >
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              aria-label={failed ? "移除" : "取消上传"}
              onClick={onCancel}
              className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/60 absolute top-1.5 right-1.5 grid size-6 place-items-center rounded-full outline-none focus-visible:ring-2"
            />
          }
        >
          <X className="size-3.5" />
        </TooltipTrigger>
        <TooltipContent>{failed ? "移除" : "取消上传"}</TooltipContent>
      </Tooltip>

      {failed ? (
        <AlertCircle className="text-destructive size-6" />
      ) : (
        <Loader2 className="text-muted-foreground size-6 animate-spin motion-reduce:animate-none" />
      )}
      <span className="block w-full truncate text-xs font-medium">{task.file.name}</span>

      {failed ? (
        <>
          <span role="alert" className="text-destructive line-clamp-2 text-xs">
            {task.error}
          </span>
          <Button variant="outline" size="xs" onClick={onRetry}>
            <RotateCcw />
            重试
          </Button>
        </>
      ) : (
        <>
          <span
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={queued ? 0 : task.percent}
            className="bg-border block h-1 w-4/5 overflow-hidden rounded-full"
          >
            <motion.span
              initial={false}
              animate={{ scaleX: queued ? 0 : task.percent / 100 }}
              transition={SPRING}
              style={{ transformOrigin: "left" }}
              className="bg-foreground/70 block size-full rounded-full"
            />
          </span>
          <span className="text-muted-foreground text-xs tabular-nums">
            {queued ? "等待上传" : processing ? "处理中…" : `${task.percent}%`}
          </span>
        </>
      )}
    </div>
  );
}
