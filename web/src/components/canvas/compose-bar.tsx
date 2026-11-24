import { Combine, Download, Loader2, PackageOpen, TriangleAlert } from "lucide-react";

import type { CanvasComposeJob } from "@/api/compose/type";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { downloadMedia } from "@/utils/canvas/download";
import { mmss, seqText, type ComposeCandidate } from "@/utils/storyboard/compose";

/**
 * 成片合成条：统计（已就绪片段 / 时长 / 状态）、跳过提示、进度、合成 / 取消、下载、打包素材。
 * 版式照成片页的合成面板（统计三宫格 + 底栏主按钮），只是从竖向 aside 压成横向一条。
 *
 * 两个入口共用：分镜表弹窗底栏，和画布上的「成片合成」节点底部。
 * 它只认「有序候选 + 任务状态」，不关心素材从哪儿来。
 */
export function ComposeBar({
  total,
  candidates,
  missing,
  job,
  busy,
  pending,
  onCompose,
  onCancel,
  onArchive,
  archiving,
}: {
  total: number;
  candidates: ComposeCandidate[];
  missing: number[];
  job: CanvasComposeJob | null;
  busy: boolean;
  pending: boolean;
  onCompose: () => void;
  onCancel: () => void;
  /** 按节点归类打包下载这张画布的全部素材；不传则不显示按钮 */
  onArchive?: () => void;
  /** 正在申请打包票据 */
  archiving?: boolean;
}) {
  const ready = candidates.length;
  const succeeded = job?.status === "succeeded";
  const stale = succeeded && job?.stale === true;
  const url = succeeded ? job?.result_asset_url : undefined;
  const canCompose = ready > 0 && !busy && !pending;
  const failed = job?.status === "failed";

  return (
    <div className="nodrag border-border bg-card flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-t px-4 py-2.5">
      <div className="flex items-center gap-2.5 text-[12.5px]">
        <span className="font-mono text-[13px] font-semibold">
          {ready}/{total}
        </span>
        <span className="text-muted-foreground">片段可合成</span>
        {job && job.duration_ms > 0 && (
          <span className="text-muted-foreground font-mono">{mmss(job.duration_ms)}</span>
        )}
        {succeeded && (
          <span
            className={cn(
              "rounded-md px-1.5 py-0.5 text-[11px] font-medium",
              stale ? "bg-primary/15 text-primary" : "bg-status-success/15 text-status-success",
            )}
          >
            {stale ? "成片已过期，可重新合成" : "成片已合成"}
          </span>
        )}
        {failed && job?.error && (
          <span className="text-destructive max-w-[28ch] truncate" title={job.error}>
            {job.error}
          </span>
        )}
      </div>

      {missing.length > 0 && (
        <span
          className="text-muted-foreground inline-flex items-center gap-1.5 text-[12px]"
          title={`${seqText(missing)} 还没有视频，合成时会跳过`}
        >
          <TriangleAlert className="text-primary size-3.5 shrink-0" />
          跳过 {seqText(missing)}
        </span>
      )}

      {busy && (
        <div className="flex min-w-[140px] flex-1 items-center gap-2">
          <span className="text-primary shrink-0 text-[12.5px] font-medium">正在合成…</span>
          {/* ffmpeg 不分段上报，不编造百分比：只给一条流动的进度条 */}
          <span className="bg-muted h-1 flex-1 overflow-hidden rounded-full">
            <span className="bg-primary block h-full w-1/3 animate-pulse rounded-full" />
          </span>
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground shrink-0 text-[12px] underline-offset-2 hover:underline"
            onClick={onCancel}
          >
            取消
          </button>
        </div>
      )}

      <span className="flex-1" />

      {/* 底栏宽度有限（成片节点只有 500 出头）：次要操作只留图标，说法挂在 title / aria-label 上，
          否则按钮一多就会被 flex-wrap 挤到第二行，看起来像排版错位 */}
      {onArchive && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={archiving}
          onClick={onArchive}
          aria-label="打包素材"
          title="把这张画布的剧本、分镜表、角色设定和全部素材按节点归类打成一个 zip"
        >
          {archiving ? <Loader2 className="animate-spin" /> : <PackageOpen />}
        </Button>
      )}
      {url && (
        <Button
          variant="outline"
          size="icon-sm"
          onClick={() => void downloadMedia(url, "成片.mp4")}
          aria-label="下载成片"
          title="下载成片 mp4"
        >
          <Download />
        </Button>
      )}
      <Button
        size="sm"
        disabled={!canCompose}
        title={reason(canCompose, busy, pending)}
        onClick={onCompose}
      >
        {pending || busy ? <Loader2 className="animate-spin" /> : <Combine />}
        {succeeded ? "重新合成" : "合成成片"}
      </Button>
    </div>
  );
}

/** 主按钮不能点时的说明 */
const reason = (canCompose: boolean, busy: boolean, pending: boolean) => {
  if (canCompose) return "按镜头顺序拼接已生成的视频，不扣积分";
  if (busy) return "正在合成，完成后可再次合成";
  if (pending) return "正在提交";
  return "至少要有一个镜头已经出了视频";
};
