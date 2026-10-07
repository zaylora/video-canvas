import { Download, FileText, RotateCcw, Trash2 } from "lucide-react";

import type { SkillVersionHead } from "@/api/admin/agent-skill/type.d";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import {
  Timeline,
  TimelineContent,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
} from "@/components/admin-ui/timeline";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { formatDateTime, formatSize, versionState } from "@/utils/admin/agent-skill";

/** 时间线上的操作回调 */
export type VersionActions = {
  /** 设为生效（回滚也是它） */
  onActivate: (version: SkillVersionHead) => void;
  /** 在“文件”页签里查看这个版本 */
  onViewFiles: (version: SkillVersionHead) => void;
  /** 下载整包 */
  onDownload: (version: SkillVersionHead) => void;
  /** 删除（整包被删，不可恢复） */
  onDelete: (version: SkillVersionHead) => void;
};

/**
 * 版本时间线（最新在上）：生效中绿边，比生效版本新的待生效为琥珀圆点，其余为历史版本。
 * 生效版本的“设为生效”“删除”禁用，删除悬停写原因。
 * @param versions 版本列表（后端已按版本号倒序）
 * @param activeVersion 当前生效版本；没有为 null
 * @param busyVersion 正在切换或删除的版本，对应卡片的按钮禁用
 * @param actions 各项操作
 */
export function VersionTimeline({
  versions,
  activeVersion,
  busyVersion,
  actions,
}: {
  versions: readonly SkillVersionHead[];
  activeVersion: number | null;
  busyVersion: number | null;
  actions: VersionActions;
}) {
  return (
    <Timeline data-slot="version-timeline">
      {versions.map((version) => {
        const state = versionState(version.version, activeVersion);
        const busy = busyVersion === version.version;
        return (
          <TimelineItem key={version.version} className="gap-3 pb-3">
            <TimelineIndicator
              className={cn(
                state === "active" && "border-emerald-500 bg-emerald-500 text-white",
                state === "pending" && "border-amber-500",
              )}
            />
            <TimelineContent
              className={cn(
                "rounded-xl border px-3.5 py-3",
                state === "active" && "border-emerald-500/40 bg-emerald-500/5",
              )}
            >
              <TimelineHeader>
                <span className="font-mono text-base font-semibold tabular-nums">
                  v{version.version}
                </span>
                {state === "active" && <Tag tone="success">生效中</Tag>}
                {state === "pending" && <Tag tone="warning">待生效</Tag>}
                <span className="text-muted-foreground ml-auto text-xs tabular-nums">
                  {formatDateTime(version.created_at)}
                </span>
              </TimelineHeader>
              <p className="text-muted-foreground mt-1.5 text-xs tabular-nums">
                <span className="font-mono" title={version.sha256}>
                  {version.sha256.slice(0, 8)}
                </span>
                {" · "}
                {version.file_count} 个文件 · {formatSize(version.total_bytes)}
              </p>
              <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
                <Button
                  size="sm"
                  variant={state === "active" ? "ghost" : "outline"}
                  disabled={state === "active" || busy}
                  onClick={() => actions.onActivate(version)}
                >
                  <RotateCcw />
                  {state === "history" ? "回滚到此版" : "设为生效"}
                </Button>
                <Button size="sm" variant="ghost" onClick={() => actions.onViewFiles(version)}>
                  <FileText />
                  文件
                </Button>
                <Button size="sm" variant="ghost" onClick={() => actions.onDownload(version)}>
                  <Download />
                  下载
                </Button>
                <ReasonTooltip
                  reason={state === "active" ? "生效版本不能删除，请先切换到其他版本" : null}
                  className="ml-auto"
                >
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive hover:text-destructive"
                    disabled={state === "active" || busy}
                    onClick={() => actions.onDelete(version)}
                  >
                    <Trash2 />
                    删除
                  </Button>
                </ReasonTooltip>
              </div>
            </TimelineContent>
          </TimelineItem>
        );
      })}
    </Timeline>
  );
}
