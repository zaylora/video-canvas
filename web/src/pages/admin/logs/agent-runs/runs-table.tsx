import { Download, Loader2 } from "lucide-react";

import type { AgentRunItem } from "@/api/admin/agent-runs/type";
import { TableSkeletonRows } from "@/components/admin-ui/table-skeleton-rows";
import { Tag } from "@/components/admin-ui/tag";
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
import {
  agentModeLabel,
  agentRunStatusView,
  creditsText,
  durationText,
  traceDownloadView,
} from "@/utils/admin/agent-runs";
import { formatFullTime } from "@/utils/admin/log-rules";

/** 每格上下两行：主信息 + 次要小字 */
function Cell({ main, sub }: { main: React.ReactNode; sub?: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <div className="text-sm">{main}</div>
      {sub ? <div className="text-muted-foreground text-xs">{sub}</div> : null}
    </div>
  );
}

/**
 * 「下载轨迹包」一格：
 * 不可下载的行不隐藏，按钮置灰，原因写在按钮下面（已过期 / 轨迹记录已关闭）；
 * 运行没结束的行仍可下载，按钮下标「未完成」；下载中按钮显示「打包中」并禁用。
 */
function DownloadCell({
  run,
  busy,
  onDownload,
}: {
  run: AgentRunItem;
  busy: boolean;
  onDownload: (run: AgentRunItem) => void;
}) {
  const view = traceDownloadView(run);
  return (
    <div className="flex flex-col items-end gap-0.5">
      <Button
        variant="outline"
        size="xs"
        disabled={!view.canDownload || busy}
        onClick={() => onDownload(run)}
      >
        {busy ? <Loader2 className="animate-spin" /> : <Download />}
        {busy ? "打包中" : "下载轨迹包"}
      </Button>
      {view.reason && <span className="text-muted-foreground text-xs">{view.reason}</span>}
      {view.unfinished && <span className="text-status-warning text-xs">未完成</span>}
    </div>
  );
}

/**
 * Agent 运行表格：时间（附 run id）/ 用户 / 画布 / 模式 / 状态 / 步数 / 报错 / 积分 / 耗时 / prompt / 来源 / 下载。
 * 报错数大于 0 标红，方便一眼找出有问题的运行；harness 跑的用例带「harness」标签，轨迹不完整的行带「不完整」标签。
 * @param loading 首次加载显示骨架行
 * @param busy 正在打包下载的 run id
 */
function RunsTable({
  items,
  loading,
  busy,
  onDownload,
}: {
  items: AgentRunItem[];
  loading: boolean;
  busy: ReadonlySet<string>;
  onDownload: (run: AgentRunItem) => void;
}) {
  return (
    <Table data-slot="agent-runs-table">
      <TableHeader>
        <TableRow className="bg-muted/50 hover:bg-muted/50">
          <TableHead className="pl-4">时间</TableHead>
          <TableHead>用户</TableHead>
          <TableHead>画布</TableHead>
          <TableHead>模式</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="text-right">步数</TableHead>
          <TableHead className="text-right">报错</TableHead>
          <TableHead className="text-right">积分</TableHead>
          <TableHead className="text-right">耗时</TableHead>
          <TableHead>prompt</TableHead>
          <TableHead>来源</TableHead>
          <TableHead className="pr-4 text-right">轨迹包</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableSkeletonRows columns={12} rows={8} />
        ) : (
          items.map((run) => {
            const status = agentRunStatusView(run.status);
            const view = traceDownloadView(run);
            return (
              <TableRow key={run.id}>
                <TableCell className="pl-4 align-top whitespace-nowrap">
                  <Cell
                    main={
                      <span className="font-mono tabular-nums">
                        {formatFullTime(run.created_at)}
                      </span>
                    }
                    sub={<span className="font-mono">run {run.id}</span>}
                  />
                </TableCell>
                <TableCell className="max-w-48 align-top">
                  <span className="block truncate text-sm" title={run.user_email}>
                    {run.user_email || run.user_id}
                  </span>
                </TableCell>
                <TableCell className="max-w-56 align-top">
                  <Cell
                    main={
                      <span className="inline-flex max-w-full items-center gap-1.5">
                        <span className="truncate" title={run.canvas_title}>
                          {run.canvas_title || "未命名画布"}
                        </span>
                        {view.incomplete && <Tag tone="warning">不完整</Tag>}
                      </span>
                    }
                    sub={<span className="font-mono">{run.canvas_id}</span>}
                  />
                </TableCell>
                <TableCell className="align-top whitespace-nowrap">
                  {agentModeLabel(run.mode)}
                </TableCell>
                <TableCell className="align-top">
                  <Tag tone={status.tone}>{status.label}</Tag>
                </TableCell>
                <TableCell className="text-right align-top tabular-nums">{run.steps}</TableCell>
                <TableCell
                  className={cn(
                    "text-right align-top tabular-nums",
                    run.tool_errors > 0 && "text-destructive font-semibold",
                  )}
                >
                  {run.tool_errors}
                </TableCell>
                <TableCell className="text-right align-top tabular-nums">
                  {creditsText(run.spent_credits)}
                </TableCell>
                <TableCell className="text-right align-top whitespace-nowrap tabular-nums">
                  {durationText(run.duration_ms)}
                </TableCell>
                <TableCell className="align-top font-mono">v{run.prompt_version}</TableCell>
                <TableCell className="max-w-40 align-top">
                  {run.source === "harness" ? (
                    <Cell
                      main={<Tag tone="violet">harness</Tag>}
                      sub={
                        run.harness_case ? (
                          <span className="block truncate font-mono" title={run.harness_case}>
                            {run.harness_case}
                          </span>
                        ) : null
                      }
                    />
                  ) : (
                    <Tag>用户</Tag>
                  )}
                </TableCell>
                <TableCell className="pr-4 text-right align-top">
                  <DownloadCell run={run} busy={busy.has(run.id)} onDownload={onDownload} />
                </TableCell>
              </TableRow>
            );
          })
        )}
      </TableBody>
    </Table>
  );
}

export { RunsTable };
