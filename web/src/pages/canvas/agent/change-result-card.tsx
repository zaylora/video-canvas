import type { ReactNode } from "react";
import { ArrowUpRight, CircleCheck, CircleX, Minus, Undo2 } from "lucide-react";
import { Link, useInRouterContext } from "react-router";

import type { AgentApprovalDto } from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { cn } from "@/lib/utils";
import {
  adminChangePayload,
  adminChangeResult,
  canRollback,
  formatDiffValue,
  resultSummary,
  rollbackMessage,
} from "@/utils/admin/assistant/change-order";
import {
  appliedEntries,
  diffKind,
  groupByPage,
  unappliedEntries,
  type AppliedEntry,
} from "@/utils/admin/assistant/change-result";

import { MoreButton, useFold } from "./fold-list";

/** 跳到后台页面：在路由里用站内跳转，没有路由（独立渲染）时退回普通链接 */
export function PageLink({
  to,
  className,
  children,
}: {
  to: string;
  className?: string;
  children: ReactNode;
}) {
  const inRouter = useInRouterContext();
  return inRouter ? (
    <Link to={to} className={className}>
      {children}
    </Link>
  ) : (
    <a href={to} className={className}>
      {children}
    </a>
  );
}

/** 一项的字段差异：字段名 + 改前（红色删除线）→ 改后（绿色）；新增只有绿色，删除只有红色删除线 */
function FieldDiff({ entry }: { entry: AppliedEntry }) {
  return (
    <dl
      aria-label="改前到改后"
      className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs"
    >
      {entry.item.diff.map((d) => {
        const kind = diffKind(d.before, d.after);
        return (
          <div key={d.path} className="col-span-2 grid grid-cols-subgrid items-baseline">
            <dt className="text-muted-foreground truncate font-mono" title={d.path}>
              {d.path}
            </dt>
            <dd className="m-0 min-w-0 wrap-anywhere">
              {kind !== "added" && (
                <span
                  className={cn(
                    "tabular-nums",
                    kind === "removed" ? "text-destructive" : "text-muted-foreground",
                    "line-through",
                  )}
                >
                  {formatDiffValue(d.before)}
                </span>
              )}
              {kind === "changed" && (
                <span aria-hidden className="text-muted-foreground mx-1.5">
                  →
                </span>
              )}
              {kind !== "removed" && (
                <span className="text-status-success font-medium tabular-nums">
                  {formatDiffValue(d.after)}
                </span>
              )}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

const STATUS_LABEL = { failed: "失败", skipped: "跳过", rejected: "未应用" } as const;

/**
 * 变更单执行后的结果卡：替代原来的一行摘要。
 * 已应用的条目按「页面 › 对象」分组，对象标题可点击跳到后台对应页面，下面逐字段列出改前 → 改后；
 * 条目超过 3 项时只露前 3 项，其余收进「还有 N 项」。失败、跳过和你没勾选的条目单独列出原因；
 * 右上角保留「回滚」。
 */
export function ChangeResultCard({
  approval,
  ctl,
}: {
  approval: AgentApprovalDto;
  ctl: AgentController;
}) {
  const payload = adminChangePayload(approval);
  const result = adminChangeResult(approval);
  const applied = payload && result ? appliedEntries(payload, result) : [];
  const fold = useFold(applied.length);
  if (!payload || !result) return null;

  const groups = groupByPage(applied.slice(0, fold.visible));
  const unapplied = unappliedEntries(payload, result);
  const ok = applied.length > 0;
  const Icon = ok ? CircleCheck : CircleX;

  return (
    <div
      role="group"
      aria-label={`变更单 ${payload.order_no} 结果`.trim()}
      className="bg-chrome-hover ring-chrome-border flex flex-col gap-2.5 rounded-2xl p-3 ring-1"
    >
      <div className="flex items-start gap-2 text-[13px]">
        <Icon
          aria-hidden
          className={cn("mt-0.5 size-4 shrink-0", ok ? "text-status-success" : "text-destructive")}
        />
        <span className="min-w-0 flex-1 leading-5 font-semibold wrap-anywhere">
          变更单 {payload.order_no} · {resultSummary(approval)}
        </span>
        {canRollback(approval) && (
          <button
            type="button"
            disabled={ctl.busy}
            title={ctl.busy ? "运行中不能回滚" : undefined}
            onClick={() => {
              if (payload.order_no) void ctl.send(rollbackMessage(payload.order_no));
            }}
            className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-2 text-xs outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-40"
          >
            <Undo2 aria-hidden className="size-3.5" />
            回滚
          </button>
        )}
      </div>

      {groups.map((group) => (
        <section key={group.page} aria-label={group.page} className="flex flex-col gap-2">
          <h4 className="text-muted-foreground text-xs font-medium">{group.page}</h4>
          <ul className="flex flex-col gap-2">
            {group.entries.map((entry) => (
              <li
                key={`${entry.item.target}:${entry.item.key}:${entry.outcome.index}`}
                className="flex flex-col gap-1"
              >
                {entry.path ? (
                  <PageLink
                    to={entry.path}
                    className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 -mx-1 inline-flex w-fit max-w-full items-center gap-1 rounded-md px-1 text-[13px] font-medium outline-none hover:underline focus-visible:ring-2"
                  >
                    <span className="min-w-0 wrap-anywhere">{entry.item.label}</span>
                    <ArrowUpRight aria-hidden className="text-muted-foreground size-3 shrink-0" />
                  </PageLink>
                ) : (
                  <span className="text-[13px] font-medium wrap-anywhere">{entry.item.label}</span>
                )}
                <FieldDiff entry={entry} />
              </li>
            ))}
          </ul>
        </section>
      ))}
      <MoreButton count={fold.hidden} onClick={fold.expand} />

      {unapplied.length > 0 && (
        <ul aria-label="未应用的条目" className="flex flex-col gap-1">
          {unapplied.map((entry, i) => (
            <li
              key={`${entry.item.target}:${entry.item.key}:${i}`}
              className={cn(
                "flex items-start gap-1.5 text-xs leading-5",
                entry.status === "failed" ? "text-destructive" : "text-muted-foreground",
              )}
            >
              <Minus aria-hidden className="mt-1 size-3 shrink-0" />
              <span className="min-w-0 wrap-anywhere">
                {STATUS_LABEL[entry.status]}：{entry.item.label}
                {entry.message && ` · ${entry.message}`}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
