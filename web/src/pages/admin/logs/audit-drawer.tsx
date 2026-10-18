import { Clock } from "lucide-react";

import type { AuditItem } from "@/api/admin/logs/type";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/admin-ui/sheet";
import { Tag } from "@/components/admin-ui/tag";
import {
  actionView,
  auditActorRole,
  auditActorView,
  auditModeLabel,
  auditSourceTable,
  auditSummary,
  auditTargetView,
  formatFullTime,
} from "@/utils/admin/log-rules";

/** 一张信息卡：标题小灰字，内容大一点，下面可带小字 */
function InfoCard({
  title,
  children,
  hint,
}: {
  title: string;
  children: React.ReactNode;
  hint?: React.ReactNode;
}) {
  return (
    <div
      data-slot="audit-info-card"
      className="bg-muted/50 flex min-w-0 flex-col gap-1 rounded-xl p-4"
    >
      <span className="text-muted-foreground text-xs">{title}</span>
      <span className="text-sm font-semibold break-words">{children}</span>
      {hint && <span className="text-muted-foreground text-xs break-words">{hint}</span>}
    </div>
  );
}

/**
 * 审计详情抽屉（右侧，不写进 URL）：结论卡（自动 / 手动 + 动作名 + 动作代码 + 时间与来源）、
 * 操作者 / 对象 / 变化三张信息卡、详情数据（detail_json 的 JSON 代码块）。全部直接展示，没有折叠块。
 * 数据来自列表行，不再请求。
 * @param item 要看的审计；关闭后保留，等关闭动画播完
 */
function AuditDrawer({
  open,
  item,
  onOpenChange,
}: {
  open: boolean;
  item: AuditItem | null;
  onOpenChange: (open: boolean) => void;
}) {
  const action = item ? actionView(item.action) : null;
  const actor = item ? auditActorView(item.actor_id, item.actor_name) : null;
  const target = item ? auditTargetView(item) : null;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent>
        <SheetHeader className="border-b pr-12">
          <SheetTitle>操作详情</SheetTitle>
          <SheetDescription className="sr-only">
            这条后台操作的时间、操作者、对象和变化
          </SheetDescription>
        </SheetHeader>

        {item && action && actor && target && (
          <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-4 pb-6">
            <div data-slot="audit-verdict" className="bg-muted flex flex-col gap-3 rounded-xl p-4">
              <div className="flex flex-wrap items-center gap-2">
                <Tag tone={actor.auto ? "info" : "neutral"}>{auditModeLabel(item.actor_id)}</Tag>
                <b className="text-base font-semibold">{action.label}</b>
                {action.legacy && <Tag>历史</Tag>}
              </div>
              <div className="bg-background flex items-center gap-2 rounded-lg border px-2 py-1.5">
                <span className="bg-muted text-muted-foreground shrink-0 rounded px-1.5 py-0.5 text-xs">
                  动作
                </span>
                <code className="min-w-0 flex-1 truncate font-mono text-xs" title={item.action}>
                  {item.action}
                </code>
                <CopyButton text={item.action} label="复制动作代码" iconOnly />
              </div>
              <div className="text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
                <span className="inline-flex items-center gap-1 tabular-nums">
                  <Clock className="size-3.5" aria-hidden />
                  {formatFullTime(item.created_at)}
                </span>
                <span>
                  来源 <span className="font-mono">{auditSourceTable(item.source)}</span>
                </span>
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-3">
              <InfoCard
                title="操作者"
                hint={`${auditActorRole(item.actor_id)}${item.actor_id > 0 ? ` · #${item.actor_id}` : ""}`}
              >
                {actor.name}
              </InfoCard>
              <InfoCard
                title="对象"
                hint={
                  <span className="inline-flex flex-wrap items-center gap-1">
                    {target.type}
                    {item.target_ref && (
                      <>
                        <span className="font-mono break-all">· {item.target_ref}</span>
                        <CopyButton text={item.target_ref} label="复制对象编号" iconOnly />
                      </>
                    )}
                  </span>
                }
              >
                {target.name}
              </InfoCard>
              <InfoCard title="变化">{auditSummary(item.action, item.detail_json)}</InfoCard>
            </div>

            <section className="flex flex-col gap-2">
              <h3 className="text-sm font-semibold">详情数据</h3>
              {item.detail_json && Object.keys(item.detail_json).length > 0 ? (
                <pre className="bg-foreground/90 text-background max-h-96 overflow-auto rounded-xl p-4 font-mono text-xs leading-5 break-all whitespace-pre-wrap">
                  {JSON.stringify(item.detail_json, null, 2)}
                </pre>
              ) : (
                <p className="text-muted-foreground text-sm">没有详情数据</p>
              )}
            </section>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

export { AuditDrawer };
