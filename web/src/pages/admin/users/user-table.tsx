import { Ban, CircleCheck, Diff, Gauge, Loader2, RotateCw, SearchX } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";

import type { UserListItem } from "@/api/admin/users/type.d";
import { AnimatedNumber } from "@/components/admin-ui/animated-number";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { InitialAvatar } from "@/components/admin-ui/initial-avatar";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { RowHint } from "@/components/admin-ui/row-icon-action";
import { StatusLabel } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { DURATION, EASE_OUT, SKELETON_DELAY, SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  concurrencyView,
  denyReason,
  formatAbsolute,
  formatRelative,
  USER_ROLE_LABEL,
  USER_ROLE_TONE,
  USER_STATUS_LABEL,
  type Actor,
} from "@/utils/admin/user-rules";

import { CreditPopover } from "./credit-popover";
import { LimitPopover } from "./limit-popover";
import type { ListState } from "./use-users";
import type { UserActions } from "./use-user-actions";
import { UserRowMenu } from "./user-row-menu";

/** 骨架行数：与首屏设计一致 */
const SKELETON_ROWS = 8;

/** 窄屏（< 768px）隐藏的列 */
const WIDE_ONLY = "max-md:hidden";

/** 等 delay 秒还没结束才返回 true：缓存命中等很快返回的场景不该闪一下骨架 */
function useDelayedFlag(active: boolean, delay: number) {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (!active) {
      setShown(false);
      return;
    }
    const timer = setTimeout(() => setShown(true), delay * 1000);
    return () => clearTimeout(timer);
  }, [active, delay]);
  return shown;
}

/** 表格的勾选状态与回调 */
export type UserSelection = {
  /** 已勾选的用户 ID */
  selected: Set<number>;
  /** 勾选 / 取消单行；shift 为 true 时连选上一次点到这一次之间的行 */
  onToggle: (id: number, checked: boolean, shift: boolean) => void;
  /** 全选 / 取消当前页 */
  onTogglePage: (checked: boolean) => void;
};

/**
 * 用户表格：勾选、用户、角色、状态、可用积分（整格是按钮）、并发、最近活跃、操作。
 * 首屏等 SKELETON_DELAY 还没返回才出 8 行骨架；翻页 / 筛选保留旧数据并半透明。
 */
function UserTable({
  items,
  state,
  refreshing,
  currentId,
  actor,
  actions,
  selection,
  filterText,
  filtering,
  onOpen,
  onRetry,
  onClearFilters,
}: {
  items: UserListItem[];
  state: ListState;
  refreshing: boolean;
  /** 弹窗里正打开的用户，行上显示竖条 */
  currentId: number | null;
  actor: Actor | null;
  actions: UserActions;
  selection: UserSelection;
  /** 当前搜索词，空结果提示用 */
  filterText: string;
  filtering: boolean;
  onOpen: (id: number) => void;
  onRetry: () => void;
  onClearFilters: () => void;
}) {
  const skeleton = useDelayedFlag(state === "loading", SKELETON_DELAY);

  if (state === "error") {
    return (
      <EmptyState className="m-4 mt-0">
        <EmptyStateTitle>加载失败</EmptyStateTitle>
        <EmptyStateDescription>用户列表没有加载出来，请检查网络后重试。</EmptyStateDescription>
        <EmptyStateActions>
          <Button variant="outline" onClick={onRetry}>
            <RotateCw />
            重试
          </Button>
        </EmptyStateActions>
      </EmptyState>
    );
  }

  if (state === "ready" && items.length === 0) {
    return (
      <EmptyState className="m-4 mt-0">
        <EmptyStateIcon>
          <SearchX />
        </EmptyStateIcon>
        <EmptyStateTitle>
          {filterText.trim() ? `没有匹配 “${filterText.trim()}” 的用户` : "没有匹配条件的用户"}
        </EmptyStateTitle>
        <EmptyStateDescription>换个关键词，或清除筛选条件。</EmptyStateDescription>
        {filtering && (
          <EmptyStateActions>
            <Button variant="outline" onClick={onClearFilters}>
              清除筛选
            </Button>
          </EmptyStateActions>
        )}
      </EmptyState>
    );
  }

  const pageIds = items.map((item) => item.id);
  const checkedCount = pageIds.filter((id) => selection.selected.has(id)).length;
  const loading = state === "loading";

  return (
    <div
      data-slot="user-table"
      className={cn("border-t transition-opacity", refreshing && !loading && "opacity-60")}
      aria-busy={refreshing}
    >
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="bg-muted/40 w-10 pl-3">
              <Checkbox
                aria-label="全选当前页"
                disabled={loading}
                checked={checkedCount > 0 && checkedCount === pageIds.length}
                indeterminate={checkedCount > 0 && checkedCount < pageIds.length}
                onCheckedChange={(checked) => selection.onTogglePage(checked)}
              />
            </TableHead>
            <TableHead className="bg-muted/40 text-muted-foreground min-w-[200px] text-xs">
              用户
            </TableHead>
            <TableHead className="bg-muted/40 text-muted-foreground w-24 text-xs">角色</TableHead>
            <TableHead className="bg-muted/40 text-muted-foreground w-[88px] text-xs">
              状态
            </TableHead>
            <TableHead className="bg-muted/40 text-muted-foreground w-[120px] text-right text-xs">
              可用积分
            </TableHead>
            <TableHead className={cn("bg-muted/40 text-muted-foreground w-28 text-xs", WIDE_ONLY)}>
              并发
            </TableHead>
            <TableHead
              className={cn("bg-muted/40 text-muted-foreground w-[104px] text-xs", WIDE_ONLY)}
            >
              最近活跃
            </TableHead>
            <TableHead className="bg-muted/40 text-muted-foreground w-28 text-right text-xs">
              操作
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading
            ? skeleton && <SkeletonRows />
            : items.map((user) => (
                <UserRow
                  key={user.id}
                  user={user}
                  current={user.id === currentId}
                  actor={actor}
                  actions={actions}
                  selection={selection}
                  onOpen={onOpen}
                />
              ))}
        </TableBody>
      </Table>
      {loading && !skeleton && <div className="h-[28rem]" aria-hidden />}
    </div>
  );
}

/** 骨架：行高与真实行一致（h-14），淡入 */
function SkeletonRows() {
  return Array.from({ length: SKELETON_ROWS }, (_, index) => (
    <motion.tr
      key={index}
      className="border-b"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
    >
      <TableCell className="h-14 w-10 pl-3">
        <Skeleton className="size-4" />
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-2.5">
          <Skeleton className="size-7 rounded-full" />
          <div className="flex flex-col gap-1.5">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-2.5 w-32" />
          </div>
        </div>
      </TableCell>
      <TableCell>
        <Skeleton className="h-5 w-14" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-4 w-12" />
      </TableCell>
      <TableCell>
        <Skeleton className="ml-auto h-4 w-10" />
      </TableCell>
      <TableCell className={WIDE_ONLY}>
        <Skeleton className="h-4 w-14" />
      </TableCell>
      <TableCell className={WIDE_ONLY}>
        <Skeleton className="h-3 w-16" />
      </TableCell>
      <TableCell />
    </motion.tr>
  ));
}

function UserRow({
  user,
  current,
  actor,
  actions,
  selection,
  onOpen,
}: {
  user: UserListItem;
  current: boolean;
  actor: Actor | null;
  actions: UserActions;
  selection: UserSelection;
  onOpen: (id: number) => void;
}) {
  const [creditOpen, setCreditOpen] = useState(false);
  const [limitOpen, setLimitOpen] = useState(false);
  /** 记录按下勾选框那一刻 Shift 有没有按着：onCheckedChange 里拿不到修饰键 */
  const shiftRef = useRef(false);
  const disabled = user.status === "disabled";
  const checked = selection.selected.has(user.id);
  const creditDeny = denyReason(actor, user, "credit");
  const limitDeny = denyReason(actor, user, "limit");
  const banDeny = denyReason(actor, user, "ban");
  const statusBusy = actions.isBusy(user.id, "status");
  const conc = concurrencyView(user);
  const self = actor?.id === user.id;

  const onKeyDown = (event: KeyboardEvent<HTMLTableRowElement>) => {
    if (event.key === "Enter" && event.target === event.currentTarget) onOpen(user.id);
  };

  /** 积分格、勾选框、操作列里的点击不该打开弹窗；浮层 portal 出去的事件也会沿 React 树冒泡到这里 */
  const stop = { onClick: (event: React.MouseEvent) => event.stopPropagation() };

  return (
    <TableRow
      data-slot="user-row"
      data-user-id={user.id}
      data-current={current || undefined}
      data-disabled={disabled || undefined}
      data-state={checked ? "selected" : undefined}
      tabIndex={0}
      aria-label={`打开 ${user.username} 的详情`}
      className={cn(
        "group/row cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/50",
        "data-[current=true]:bg-muted data-[disabled=true]:text-muted-foreground",
      )}
      onClick={() => onOpen(user.id)}
      onKeyDown={onKeyDown}
    >
      <TableCell className="relative h-14 w-10 pl-3" {...stop}>
        {current && (
          <motion.span
            layoutId="user-current-bar"
            transition={SPRING}
            className="bg-foreground absolute inset-y-0 left-0 w-0.5 rounded-r-full"
          />
        )}
        <Checkbox
          aria-label={`选择 ${user.username}`}
          checked={checked}
          onPointerDown={(event) => {
            shiftRef.current = event.shiftKey;
          }}
          onCheckedChange={(next) => {
            selection.onToggle(user.id, next, shiftRef.current);
            shiftRef.current = false;
          }}
        />
      </TableCell>
      <TableCell className="min-w-[200px]">
        <div className="flex items-center gap-2.5">
          <InitialAvatar
            name={user.username}
            seed={String(user.id)}
            className="size-7 rounded-full text-xs shadow-none"
          />
          <div className="min-w-0">
            <div className="truncate font-medium">
              {user.username}
              {self && (
                <span className="text-muted-foreground ml-1.5 text-xs font-normal">（你）</span>
              )}
            </div>
            <div className="text-muted-foreground truncate text-xs">{user.email || "—"}</div>
          </div>
        </div>
      </TableCell>
      <TableCell>
        <Tag tone={USER_ROLE_TONE[user.role]}>{USER_ROLE_LABEL[user.role]}</Tag>
      </TableCell>
      <TableCell>
        <StatusCell disabled={disabled} />
      </TableCell>
      <TableCell className="text-right" {...stop}>
        <CreditCell
          user={user}
          actions={actions}
          deny={creditDeny}
          open={creditOpen}
          onOpenChange={setCreditOpen}
        />
      </TableCell>
      <TableCell className={WIDE_ONLY}>
        <div className="flex items-center gap-2">
          <span className={cn("font-mono tabular-nums", conc.full && "text-status-warning")}>
            {conc.active}/{conc.limit}
          </span>
          <span className="bg-muted h-1 w-12 overflow-hidden rounded-full" aria-hidden>
            <motion.span
              className={cn(
                "block h-full w-full origin-left rounded-full",
                conc.full ? "bg-status-warning" : "bg-foreground",
              )}
              initial={false}
              animate={{ scaleX: conc.ratio }}
              transition={SPRING}
            />
          </span>
          {conc.custom && <span className="text-muted-foreground text-[11px]">自定</span>}
        </div>
      </TableCell>
      <TableCell className={cn("text-muted-foreground text-xs", WIDE_ONLY)}>
        <span title={formatAbsolute(user.last_login_at)}>{formatRelative(user.last_login_at)}</span>
      </TableCell>
      <TableCell className="w-28" {...stop}>
        <div className="flex items-center justify-end gap-0.5">
          <RowHint label="调整并发上限" deny={limitDeny}>
            <LimitPopover
              user={user}
              actions={actions}
              open={limitOpen}
              onOpenChange={setLimitOpen}
              align="end"
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={!!limitDeny}
                  aria-label={`调整 ${user.username} 的并发上限`}
                />
              }
            >
              <Gauge />
            </LimitPopover>
          </RowHint>
          <RowHint label={disabled ? "启用" : "封禁"} deny={banDeny}>
            <Button
              variant="ghost"
              size="icon-sm"
              disabled={!!banDeny || statusBusy}
              className={cn(!disabled && "text-destructive hover:text-destructive")}
              aria-label={`${disabled ? "启用" : "封禁"} ${user.username}`}
              onClick={() => void actions.setStatus(user, disabled ? "active" : "disabled")}
            >
              {statusBusy ? (
                <Loader2 className="animate-spin" />
              ) : disabled ? (
                <CircleCheck />
              ) : (
                <Ban />
              )}
            </Button>
          </RowHint>
          <UserRowMenu user={user} actor={actor} actions={actions} />
        </div>
      </TableCell>
    </TableRow>
  );
}

/** 状态格：正常是绿点 + 文字，已停用是 destructive Tag；封禁 / 启用时用 DURATION.fast 交叉淡入淡出 */
function StatusCell({ disabled }: { disabled: boolean }) {
  return (
    <AnimatePresence mode="popLayout" initial={false}>
      <motion.span
        key={disabled ? "disabled" : "active"}
        className="inline-flex"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        transition={{ duration: DURATION.fast, ease: EASE_OUT }}
      >
        {disabled ? (
          <Tag tone="danger">
            <Ban />
            {USER_STATUS_LABEL.disabled}
          </Tag>
        ) : (
          <StatusLabel tone="success" className="text-foreground text-sm">
            {USER_STATUS_LABEL.active}
          </StatusLabel>
        )}
      </motion.span>
    </AnimatePresence>
  );
}

/** 积分格内容：主数字 + 有冻结时下方「冻结 N」，两个数字都会滚动 */
function CreditValue({ user }: { user: UserListItem }) {
  return (
    <>
      <AnimatedNumber value={user.available} className="font-mono font-medium" />
      {user.frozen > 0 && (
        <span className="text-muted-foreground text-xs tabular-nums">
          冻结 <AnimatedNumber value={user.frozen} />
        </span>
      )}
    </>
  );
}

const CREDIT_CELL =
  "group/credit relative -mr-2 inline-flex flex-col items-end rounded-lg border border-dashed border-transparent px-2 py-1 text-right outline-none transition-colors hover:border-foreground/30 hover:bg-background focus-visible:ring-3 focus-visible:ring-ring/50 data-popup-open:border-foreground data-popup-open:bg-background";

/** 整格是按钮：hover 出现虚线描边与 ± 图标，点击弹出积分浮层；无权限时禁用并说明原因 */
function CreditCell({
  user,
  actions,
  deny,
  open,
  onOpenChange,
}: {
  user: UserListItem;
  actions: UserActions;
  deny: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  if (deny) {
    return (
      <ReasonTooltip reason={deny}>
        <button type="button" disabled className={cn(CREDIT_CELL, "cursor-default")}>
          <CreditValue user={user} />
        </button>
      </ReasonTooltip>
    );
  }
  return (
    <CreditPopover
      user={user}
      actions={actions}
      open={open}
      onOpenChange={onOpenChange}
      render={
        <motion.button
          type="button"
          whileTap={TAP}
          className={CREDIT_CELL}
          aria-label={`调整 ${user.username} 的积分`}
        />
      }
    >
      <Diff
        className="text-muted-foreground pointer-events-none absolute top-1/2 left-0 size-3.5 -translate-x-full -translate-y-1/2 opacity-0 transition-opacity group-hover/credit:opacity-100 group-data-popup-open/credit:opacity-100"
        aria-hidden
      />
      <CreditValue user={user} />
    </CreditPopover>
  );
}

export { UserTable };
