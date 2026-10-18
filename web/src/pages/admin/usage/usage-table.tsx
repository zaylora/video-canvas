import { Clapperboard, Eye, FlaskConical, Image as ImageIcon, Music, Type } from "lucide-react";
import type { KeyboardEvent, ReactNode } from "react";

import type { UsageItem } from "@/api/admin/usage/type";
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
import { DURATION, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  agoText,
  creditCell,
  durationCell,
  formatFullTime,
  isActiveStatus,
  kindTagView,
  statusDetail,
  usageStatusView,
} from "@/utils/admin/log-rules";

import { TONE_DOT } from "./tone";

const KIND_ICON = {
  video: Clapperboard,
  image: ImageIcon,
  audio: Music,
  text: Type,
  other: Type,
} as const;

const CREDIT_CLASS = {
  success: "text-status-success font-medium",
  muted: "text-muted-foreground",
  plain: "",
} as const;

/** 每格上下两行：主信息 + 次要小字 */
function Cell({ main, sub }: { main: ReactNode; sub?: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <div className="text-sm">{main}</div>
      {sub ? <div className="text-muted-foreground text-xs">{sub}</div> : null}
    </div>
  );
}

/**
 * 使用记录表格：用户 / 类型 / 模型 / 状态 / 积分 / 耗时 / 时间 / 操作，每格上下两行。
 * 类型是彩色小标签，积分成功时用绿色强调，耗时单独成列并对齐数字；进行中的状态点脉动、耗时每秒增长。
 * 整行可点（Enter / Space 同样）打开详情抽屉，最右列「详情」按钮效果相同；点用户名只加入用户筛选。
 * @param loading 首次加载显示骨架行
 * @param activeRef 抽屉里正在看的任务，对应行高亮
 */
function UsageTable({
  items,
  loading,
  activeRef,
  onOpen,
  onFilterUser,
}: {
  items: UsageItem[];
  loading: boolean;
  activeRef: string | null;
  onOpen: (ref: string) => void;
  onFilterUser: (user: UsageItem["user"]) => void;
}) {
  const now = useNow(items.some((item) => isActiveStatus(item.status)));
  const onKey = (event: KeyboardEvent, ref: string) => {
    if (event.target !== event.currentTarget) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onOpen(ref);
    }
  };
  return (
    <Table data-slot="usage-table">
      <TableHeader>
        <TableRow className="bg-muted/50 hover:bg-muted/50">
          <TableHead className="pl-4">用户</TableHead>
          <TableHead>类型</TableHead>
          <TableHead>模型</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="text-right">积分</TableHead>
          <TableHead className="text-right">耗时</TableHead>
          <TableHead>时间</TableHead>
          <TableHead className="pr-4 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableSkeletonRows columns={8} rows={8} />
        ) : (
          items.map((item) => {
            const status = usageStatusView(item.status);
            const kind = kindTagView(item.kind);
            const KindIcon = KIND_ICON[kind.icon];
            const credit = creditCell(item);
            const detail = statusDetail(item);
            const modelName = item.model_name || item.model_key;
            return (
              <TableRow
                key={item.id}
                tabIndex={0}
                aria-current={item.task_ref === activeRef ? "true" : undefined}
                className={cn(
                  "cursor-pointer transition-colors",
                  item.task_ref === activeRef && "bg-muted/60",
                )}
                style={{ transitionDuration: ms(DURATION.fast) }}
                onClick={() => onOpen(item.task_ref)}
                onKeyDown={(event) => onKey(event, item.task_ref)}
              >
                <TableCell className="pl-4 align-top">
                  <Cell
                    main={
                      <button
                        type="button"
                        title="只看这个用户"
                        className="hover:text-primary focus-visible:ring-ring/50 rounded-sm font-medium underline-offset-2 outline-none hover:underline focus-visible:ring-2"
                        onClick={(event) => {
                          event.stopPropagation();
                          onFilterUser(item.user);
                        }}
                        onKeyDown={(event) => event.stopPropagation()}
                      >
                        {item.user.username}
                      </button>
                    }
                    sub={
                      item.is_test ? (
                        <Tag tone="violet">
                          <FlaskConical className="size-3" />
                          试跑
                        </Tag>
                      ) : null
                    }
                  />
                </TableCell>
                <TableCell className="align-top">
                  <Tag tone={kind.tone}>
                    <KindIcon className="size-3" aria-hidden />
                    {kind.label}
                  </Tag>
                </TableCell>
                <TableCell className="max-w-48 align-top">
                  <Cell
                    main={
                      <span className="block truncate" title={modelName}>
                        {modelName}
                      </span>
                    }
                    sub={
                      item.model_key !== modelName ? (
                        <span className="block truncate font-mono" title={item.model_key}>
                          {item.model_key}
                        </span>
                      ) : null
                    }
                  />
                </TableCell>
                <TableCell className="max-w-56 align-top">
                  <Cell
                    main={
                      <span className="inline-flex items-center gap-1.5">
                        <span
                          aria-hidden
                          className={cn(
                            "size-2 shrink-0 rounded-full",
                            TONE_DOT[status.tone],
                            status.active && "animate-pulse motion-reduce:animate-none",
                          )}
                        />
                        {status.label}
                      </span>
                    }
                    sub={detail ? <span className="line-clamp-2 break-words">{detail}</span> : null}
                  />
                </TableCell>
                <TableCell
                  className={cn(
                    "text-right align-top text-sm whitespace-nowrap tabular-nums",
                    CREDIT_CLASS[credit.tone],
                  )}
                >
                  {credit.text}
                </TableCell>
                <TableCell className="text-right align-top text-sm whitespace-nowrap tabular-nums">
                  {durationCell(item, now)}
                </TableCell>
                <TableCell className="align-top whitespace-nowrap">
                  <Cell
                    main={
                      <span className="font-mono tabular-nums">
                        {formatFullTime(item.created_at)}
                      </span>
                    }
                    sub={agoText(item.created_at, now)}
                  />
                </TableCell>
                <TableCell className="pr-4 text-right align-top">
                  <Button
                    variant="ghost"
                    size="xs"
                    className="text-primary hover:text-primary"
                    onClick={(event) => {
                      event.stopPropagation();
                      onOpen(item.task_ref);
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

export { UsageTable };
