import { Eye } from "lucide-react";
import type { KeyboardEvent } from "react";

import type { AuditItem } from "@/api/admin/logs/type";
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
import { useNow } from "@/hooks/use-now";
import {
  actionView,
  agoText,
  auditActorRole,
  auditActorView,
  auditSummary,
  auditTargetView,
  formatFullTime,
} from "@/utils/admin/log-rules";

/** 每格上下两行：主信息 + 次要小字 */
function Cell({ main, sub }: { main: React.ReactNode; sub: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <div className="text-sm">{main}</div>
      <div className="text-muted-foreground text-xs">{sub}</div>
    </div>
  );
}

/**
 * 操作审计表格：时间 / 操作者 / 动作 / 对象 / 变化 / 操作，每格上下两行。
 * 整行可点（Enter / Space 同样）打开抽屉，最右一列的「详情」按钮效果相同。
 * 只用审计已有的字段：操作者第二行写「后台管理员 / 系统自动」，动作第二行是动作代码。
 * @param loading 首次加载显示骨架行
 */
function AuditTable({
  items,
  loading,
  onOpen,
}: {
  items: AuditItem[];
  loading: boolean;
  onOpen: (item: AuditItem) => void;
}) {
  const now = useNow(false);
  const onKey = (event: KeyboardEvent, item: AuditItem) => {
    if (event.target !== event.currentTarget) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onOpen(item);
    }
  };
  return (
    <Table data-slot="audit-table">
      <TableHeader>
        <TableRow className="bg-muted/50 hover:bg-muted/50">
          <TableHead className="pl-4">时间</TableHead>
          <TableHead>操作者</TableHead>
          <TableHead>动作</TableHead>
          <TableHead>对象</TableHead>
          <TableHead>变化</TableHead>
          <TableHead className="pr-4 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableSkeletonRows columns={6} rows={8} />
        ) : (
          items.map((item) => {
            const actor = auditActorView(item.actor_id, item.actor_name);
            const action = actionView(item.action);
            const target = auditTargetView(item);
            const summary = auditSummary(item.action, item.detail_json);
            return (
              <TableRow
                key={`${item.source}:${item.id}`}
                tabIndex={0}
                className="cursor-pointer"
                onClick={() => onOpen(item)}
                onKeyDown={(event) => onKey(event, item)}
              >
                <TableCell className="pl-4 align-top whitespace-nowrap">
                  <Cell
                    main={
                      <span className="font-mono tabular-nums">
                        {formatFullTime(item.created_at)}
                      </span>
                    }
                    sub={agoText(item.created_at, now)}
                  />
                </TableCell>
                <TableCell className="align-top">
                  <Cell
                    main={
                      <span className="inline-flex items-center gap-1.5">
                        {actor.name}
                        {actor.auto && <Tag>自动</Tag>}
                      </span>
                    }
                    sub={auditActorRole(item.actor_id)}
                  />
                </TableCell>
                <TableCell className="max-w-56 align-top">
                  <Cell
                    main={
                      <span className="inline-flex flex-wrap items-center gap-1.5">
                        {action.label}
                        {action.legacy && <Tag>历史</Tag>}
                      </span>
                    }
                    sub={
                      <span className="block truncate font-mono" title={item.action}>
                        {item.action}
                      </span>
                    }
                  />
                </TableCell>
                <TableCell className="max-w-48 align-top">
                  <Cell
                    main={
                      <span className="block truncate" title={target.name}>
                        {target.name}
                      </span>
                    }
                    sub={target.type}
                  />
                </TableCell>
                <TableCell className="max-w-72 align-top">
                  <span
                    className="text-muted-foreground line-clamp-2 text-sm break-words whitespace-normal"
                    title={summary}
                  >
                    {summary}
                  </span>
                </TableCell>
                <TableCell className="pr-4 text-right align-top">
                  <Button
                    variant="ghost"
                    size="xs"
                    className="text-primary hover:text-primary"
                    onClick={(event) => {
                      event.stopPropagation();
                      onOpen(item);
                    }}
                    onKeyDown={(event) => event.stopPropagation()}
                  >
                    <Eye />
                    详情
                  </Button>
                </TableCell>
              </TableRow>
            );
          })
        )}
      </TableBody>
    </Table>
  );
}

export { AuditTable };
