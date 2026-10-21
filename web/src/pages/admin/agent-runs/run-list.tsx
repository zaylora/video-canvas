import { SearchX, ThumbsDown, ThumbsUp } from "lucide-react";
import type { KeyboardEvent } from "react";

import type { AgentRunDigest } from "@/api/admin/agent-run/type.d";
import { DataPanelBody } from "@/components/admin-ui/data-panel";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { Notice } from "@/components/admin-ui/notice";
import { TableSkeletonRows } from "@/components/admin-ui/table-skeleton-rows";
import { Tag, type TagTone } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { harnessLabel, runRowBadges, runStatusView } from "@/utils/admin/agent-run";
import { formatDateTimeSec } from "@/utils/admin/log-rules";

/** 列表加载状态：loading 首屏加载中，ready 已有数据（可能为空），error 首屏加载失败 */
export type RunListState = "loading" | "ready" | "error";

/** 徽标的色调 */
const BADGE_TONE: Record<string, TagTone> = {
  tool_errors: "danger",
  steer: "info",
  rejected: "warning",
};

/** 反馈列：👍 / 👎 图标，没评价是「—」 */
function FeedbackCell({ feedback }: { feedback: AgentRunDigest["feedback"] }) {
  if (feedback === 1)
    return (
      <span title="有帮助" className="text-status-success inline-flex">
        <ThumbsUp className="size-4" aria-hidden="true" />
        <span className="sr-only">有帮助</span>
      </span>
    );
  if (feedback === -1)
    return (
      <span title="没帮上忙" className="text-destructive inline-flex">
        <ThumbsDown className="size-4" aria-hidden="true" />
        <span className="sr-only">没帮上忙</span>
      </span>
    );
  return <span className="text-muted-foreground">—</span>;
}

/** 一行运行；整行可点（Enter / Space 同样）打开详情抽屉 */
function RunRow({
  run,
  active,
  onOpen,
}: {
  run: AgentRunDigest;
  active: boolean;
  onOpen: (rid: string) => void;
}) {
  const status = runStatusView(run.status);
  const badges = runRowBadges(run);
  const onKey = (event: KeyboardEvent) => {
    if (event.target !== event.currentTarget) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onOpen(run.run_id);
    }
  };
  return (
    <TableRow
      data-run-id={run.run_id}
      tabIndex={0}
      aria-current={active ? "true" : undefined}
      className={cn("cursor-pointer transition-colors", active && "bg-muted/60")}
      onClick={() => onOpen(run.run_id)}
      onKeyDown={onKey}
    >
      <TableCell className="pl-4 font-mono text-xs whitespace-nowrap tabular-nums">
        {formatDateTimeSec(run.created_at)}
      </TableCell>
      <TableCell className="text-sm whitespace-nowrap tabular-nums">#{run.user_id}</TableCell>
      <TableCell className="max-w-80 min-w-48">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm" title={run.request_head}>
            {run.request_head || "—"}
          </span>
          {badges.map((badge) => (
            <Tag
              key={badge.key}
              tone={BADGE_TONE[badge.key]}
              title={badge.title}
              className="shrink-0 px-1 text-[11px] tabular-nums"
            >
              {badge.text}
            </Tag>
          ))}
        </div>
      </TableCell>
      <TableCell>
        <Tag tone={status.tone} title={run.error_code || undefined}>
          {status.label}
        </Tag>
      </TableCell>
      <TableCell className="text-right text-sm tabular-nums">{run.steps}</TableCell>
      <TableCell className="text-right text-sm tabular-nums">{run.spent_credits}</TableCell>
      <TableCell>
        <FeedbackCell feedback={run.feedback} />
      </TableCell>
      <TableCell className="text-sm">
        {run.undone ? (
          <span className="text-status-warning font-medium">是</span>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </TableCell>
      <TableCell className="pr-4">
        <span
          title={harnessLabel(run.prompt_version, run.harness_key)}
          className="font-mono text-xs tabular-nums"
        >
          v{run.prompt_version}
        </span>
      </TableCell>
    </TableRow>
  );
}

/**
 * 运行列表：时间、用户、请求首句（旁边是工具报错 / 插话 / 审批被拒的小徽标）、状态、步数、积分、反馈、撤销、版本。
 * 加载中给骨架行（表头照常显示）；失败给 Notice 加重试；没有符合条件的运行给空状态，有筛选时带「重置筛选」。
 * @param state 加载状态
 * @param items 当前页的运行
 * @param refreshing 正在换页或换筛选：保留旧数据，表格半透明
 * @param activeRid 抽屉里正在看的运行，对应行高亮
 * @param filtersActive 有没有筛选在生效
 */
export function RunList({
  state,
  refreshing = false,
  items,
  activeRid,
  filtersActive,
  onOpen,
  onRetry,
  onReset,
}: {
  state: RunListState;
  refreshing?: boolean;
  items: AgentRunDigest[];
  activeRid: string | null;
  filtersActive: boolean;
  onOpen: (rid: string) => void;
  onRetry: () => void;
  onReset: () => void;
}) {
  if (state === "error")
    return (
      <DataPanelBody>
        <Notice
          tone="danger"
          title="运行记录加载失败"
          className="m-4"
          action={
            <Button variant="outline" size="sm" onClick={onRetry}>
              重试
            </Button>
          }
        />
      </DataPanelBody>
    );
  if (state === "ready" && items.length === 0)
    return (
      <DataPanelBody>
        <EmptyState className="m-4">
          <EmptyStateIcon>
            <SearchX />
          </EmptyStateIcon>
          <EmptyStateTitle>没有符合条件的运行</EmptyStateTitle>
          {filtersActive && (
            <EmptyStateActions>
              <Button variant="outline" size="sm" onClick={onReset}>
                重置筛选
              </Button>
            </EmptyStateActions>
          )}
        </EmptyState>
      </DataPanelBody>
    );
  return (
    <DataPanelBody>
      <Table data-slot="run-table" className={cn("transition-opacity", refreshing && "opacity-60")}>
        <TableHeader>
          <TableRow className="bg-muted/50 hover:bg-muted/50">
            <TableHead className="pl-4">时间</TableHead>
            <TableHead>用户</TableHead>
            <TableHead>请求首句</TableHead>
            <TableHead>状态</TableHead>
            <TableHead className="text-right">步数</TableHead>
            <TableHead className="text-right">积分</TableHead>
            <TableHead>反馈</TableHead>
            <TableHead>撤销</TableHead>
            <TableHead className="pr-4">版本</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {state === "loading" ? (
            <TableSkeletonRows columns={9} rows={8} />
          ) : (
            items.map((run) => (
              <RunRow
                key={run.run_id}
                run={run}
                active={run.run_id === activeRid}
                onOpen={onOpen}
              />
            ))
          )}
        </TableBody>
      </Table>
    </DataPanelBody>
  );
}
