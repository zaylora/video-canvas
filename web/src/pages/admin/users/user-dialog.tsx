import {
  Ban,
  ChevronDown,
  ChevronUp,
  CircleCheck,
  Gauge,
  Loader2,
  Diff,
  RotateCw,
  UserX,
  X,
} from "lucide-react";
import { motion } from "motion/react";
import { useState } from "react";

import type { UserDetail, UserListItem } from "@/api/admin/users/type.d";
import { AnimatedNumber } from "@/components/admin-ui/animated-number";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { InitialAvatar } from "@/components/admin-ui/initial-avatar";
import { MotionButton } from "@/components/admin-ui/motion-button";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useRetained } from "@/hooks/use-retained";
import { DURATION, EASE_OUT, SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  concurrencyView,
  denyReason,
  formatMonthDay,
  USER_ROLE_LABEL,
  USER_ROLE_TONE,
  USER_STATUS_LABEL,
  type Actor,
} from "@/utils/admin/user-rules";

import { CreditPopover } from "./credit-popover";
import { LimitPopover } from "./limit-popover";
import type { DetailState, UserTab } from "./use-users";
import type { UserActions } from "./use-user-actions";
import { CreditsTab, LoginsTab, OverviewTab, TasksTab } from "./user-dialog-tabs";
import { UserRowMenu } from "./user-row-menu";

const TAB_LABEL: Record<UserTab, string> = {
  overview: "概览",
  tasks: "生成",
  credits: "积分",
  logins: "登录",
};

const TAB_ORDER: UserTab[] = ["overview", "tasks", "credits", "logins"];

/** 弹窗：固定高度，头部与摘要常驻，页签内容在里面滚动；窄屏左右各留 0.75rem */
const DIALOG_CLASS =
  "flex h-[min(90svh,880px)] max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl";

/**
 * 用户详情弹窗：顶部摘要常驻 + 概览 / 生成 / 积分 / 登录四个页签（页签内容独立滚动）。
 * 弹窗外框只在打开 / 关闭时缩放淡入，切换用户时只有内容按方向淡入位移 8px。
 * Esc 由页面按层级统一处理（浮层 → 勾选 → 弹窗），所以这里不响应 Base UI 自己的关闭请求。
 */
function UserDialog({
  userId,
  order,
  listItem,
  detail,
  detailState,
  tab,
  refreshKey,
  actor,
  actions,
  canPrev,
  canNext,
  onStep,
  onTabChange,
  onClose,
  onRetry,
}: {
  /** 当前打开的用户；null 表示弹窗关闭 */
  userId: number | null;
  /** 当前表格的用户顺序，用来判断切换方向 */
  order: number[];
  /** 表格里这个用户的行数据：详情没回来前先用它画头部和摘要 */
  listItem: UserListItem | null;
  detail: UserDetail | null;
  detailState: DetailState;
  tab: UserTab;
  /** 每次对这个用户做完写操作加一，页签里的列表据此重新拉取 */
  refreshKey: number;
  actor: Actor | null;
  actions: UserActions;
  canPrev: boolean;
  canNext: boolean;
  onStep: (dir: 1 | -1) => void;
  onTabChange: (tab: UserTab) => void;
  onClose: () => void;
  onRetry: () => void;
}) {
  const open = userId !== null;
  /** 关闭动画播放期间还显示最后那个用户 */
  const shownId = useRetained(userId);
  const [last, setLast] = useState<{ id: number | null; dir: -1 | 0 | 1 }>({ id: null, dir: 0 });
  if (shownId !== null && shownId !== last.id) {
    const dir = last.id === null ? 0 : order.indexOf(shownId) >= order.indexOf(last.id) ? 1 : -1;
    setLast({ id: shownId, dir });
  }

  /** 关闭动画播放期间不能让头部变回骨架：用最后一次的用户与详情撑到动画播完 */
  const liveDetail = open ? detail : null;
  const keptDetail = useRetained(liveDetail);
  const shownDetail = open ? detail : keptDetail;
  const live = open ? (detail ?? (listItem && listItem.id === userId ? listItem : null)) : null;
  const keptUser = useRetained(live);
  const user = open ? live : keptUser;

  return (
    <Dialog
      open={open}
      onOpenChange={(next, details) => {
        if (!next && details.reason === "outside-press") onClose();
      }}
    >
      <DialogContent
        showCloseButton={false}
        finalFocus={false}
        className={DIALOG_CLASS}
        aria-label="用户详情"
      >
        <DialogTitle className="sr-only">
          {user ? `${user.username} 的详情` : "用户详情"}
        </DialogTitle>
        <DialogDescription className="sr-only">
          查看用户、调整积分与并发、封禁账号
        </DialogDescription>
        {shownId !== null && (
          <motion.div
            key={shownId}
            className="flex min-h-0 flex-1 flex-col"
            initial={{ opacity: 0, y: last.dir * 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
          >
            {detailState === "notfound" ? (
              <DialogMessage icon={<UserX className="size-8" />} title="用户不存在或已被删除">
                <Button variant="outline" onClick={onClose}>
                  关闭
                </Button>
              </DialogMessage>
            ) : !user && detailState === "error" ? (
              <DialogMessage icon={<RotateCw className="size-8" />} title="加载失败">
                <Button variant="outline" onClick={onRetry}>
                  重试
                </Button>
                <Button variant="ghost" onClick={onClose}>
                  关闭
                </Button>
              </DialogMessage>
            ) : (
              <>
                <DialogHead
                  user={user}
                  canPrev={canPrev}
                  canNext={canNext}
                  onStep={onStep}
                  onClose={onClose}
                />
                <DialogSummary user={user} actor={actor} actions={actions} />
                <Tabs
                  value={tab}
                  onValueChange={(value) => onTabChange(value as UserTab)}
                  className="mt-4 min-h-0 flex-1 gap-0"
                >
                  <TabsList
                    variant="line"
                    className="h-10 w-full justify-start gap-1 rounded-none border-b px-5 group-data-horizontal/tabs:h-10"
                  >
                    {TAB_ORDER.map((value) => (
                      <TabsTrigger
                        key={value}
                        value={value}
                        className="h-full flex-none px-2 after:hidden"
                      >
                        {TAB_LABEL[value]}
                        {value === "tasks" && !!user && user.active_tasks > 0 && (
                          <span className="bg-status-running text-background inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] tabular-nums">
                            {user.active_tasks}
                          </span>
                        )}
                        {tab === value && (
                          <motion.span
                            layoutId="user-dialog-tab-line"
                            transition={SPRING}
                            className="bg-foreground absolute inset-x-0 -bottom-px h-0.5"
                          />
                        )}
                      </TabsTrigger>
                    ))}
                  </TabsList>
                  <TabsContent value="overview" className={PANEL}>
                    <Fade>
                      <OverviewTab
                        detail={shownDetail}
                        onGotoCredits={() => onTabChange("credits")}
                      />
                    </Fade>
                  </TabsContent>
                  <TabsContent value="tasks" className={PANEL}>
                    <Fade>
                      <TasksTab userId={shownId} refreshKey={refreshKey} />
                    </Fade>
                  </TabsContent>
                  <TabsContent value="credits" className={PANEL}>
                    <Fade>
                      <CreditsTab userId={shownId} refreshKey={refreshKey} />
                    </Fade>
                  </TabsContent>
                  <TabsContent value="logins" className={PANEL}>
                    <Fade>
                      <LoginsTab userId={shownId} refreshKey={refreshKey} />
                    </Fade>
                  </TabsContent>
                </Tabs>
              </>
            )}
          </motion.div>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** 页签内容区：独立滚动 */
const PANEL = "min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-6";

/** 页签内容切换只做淡入，不位移，免得和切换用户的位移混淆 */
function Fade({ children }: { children: React.ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
    >
      {children}
    </motion.div>
  );
}

function DialogMessage({
  icon,
  title,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="text-muted-foreground flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
      {icon}
      <div className="text-foreground font-medium">{title}</div>
      <div className="flex items-center gap-2">{children}</div>
    </div>
  );
}

/** 头部：头像、名字、角色 / 状态 Tag、邮箱 · ID · 注册时间，以及 上一个 / 下一个 / 关闭 */
function DialogHead({
  user,
  canPrev,
  canNext,
  onStep,
  onClose,
}: {
  user: UserListItem | null;
  canPrev: boolean;
  canNext: boolean;
  onStep: (dir: 1 | -1) => void;
  onClose: () => void;
}) {
  return (
    <div className="flex items-start gap-3 border-b px-5 pt-5 pb-4">
      {user ? (
        <InitialAvatar
          name={user.username}
          seed={String(user.id)}
          className="size-10 rounded-full text-base shadow-none"
        />
      ) : (
        <Skeleton className="size-10 rounded-full" />
      )}
      <div className="min-w-0 flex-1">
        {user ? (
          <>
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="truncate text-base font-semibold">{user.username}</span>
              <Tag tone={USER_ROLE_TONE[user.role]}>{USER_ROLE_LABEL[user.role]}</Tag>
              {user.status === "active" ? (
                <Tag tone="success">{USER_STATUS_LABEL.active}</Tag>
              ) : (
                <Tag tone="danger">
                  <Ban />
                  {USER_STATUS_LABEL.disabled}
                </Tag>
              )}
            </div>
            <div className="text-muted-foreground mt-0.5 flex flex-wrap items-center gap-x-1 text-xs">
              <span className="truncate">{user.email || "未填写邮箱"}</span>
              {user.email && <CopyButton text={user.email} label="复制邮箱" iconOnly />}
              <span className="tabular-nums">
                · ID {user.id} · 注册于 {formatMonthDay(user.created_at)}
              </span>
            </div>
          </>
        ) : (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-3 w-48" />
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-0.5">
        <StepButton label="上一个用户（↑ / K）" disabled={!canPrev} onClick={() => onStep(-1)}>
          <ChevronUp />
        </StepButton>
        <StepButton label="下一个用户（↓ / J）" disabled={!canNext} onClick={() => onStep(1)}>
          <ChevronDown />
        </StepButton>
        <StepButton label="关闭（Esc）" onClick={onClose}>
          <X />
        </StepButton>
      </div>
    </div>
  );
}

function StepButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <motion.button
            type="button"
            whileTap={disabled ? undefined : TAP}
            disabled={disabled}
            aria-label={label}
            onClick={onClick}
            className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 inline-flex size-8 items-center justify-center rounded-lg outline-none transition-colors focus-visible:ring-3 disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-4"
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** 摘要三格（可用积分 / 冻结 / 并发）与操作行（调整积分 / 并发上限 / 封禁·启用 / ⋯） */
function DialogSummary({
  user,
  actor,
  actions,
}: {
  user: UserListItem | null;
  actor: Actor | null;
  actions: UserActions;
}) {
  const [creditOpen, setCreditOpen] = useState(false);
  const [limitOpen, setLimitOpen] = useState(false);

  if (!user) {
    return (
      <div className="px-5 pt-4" aria-busy>
        <Skeleton className="h-[74px] w-full rounded-xl" />
        <Skeleton className="mt-3 h-8 w-64" />
      </div>
    );
  }

  const conc = concurrencyView(user);
  const creditDeny = denyReason(actor, user, "credit");
  const limitDeny = denyReason(actor, user, "limit");
  const banDeny = denyReason(actor, user, "ban");
  const disabled = user.status === "disabled";
  const statusBusy = actions.isBusy(user.id, "status");

  return (
    <div className="px-5 pt-4">
      <div className="grid grid-cols-3 overflow-hidden rounded-xl border">
        <div className="p-3">
          <div className="text-muted-foreground text-xs">可用积分</div>
          <AnimatedNumber
            value={user.available}
            className="text-credit mt-1 block font-mono text-2xl font-semibold"
          />
        </div>
        <div className="border-l p-3">
          <div className="text-muted-foreground text-xs">冻结</div>
          <AnimatedNumber
            value={user.frozen}
            className="mt-1 block font-mono text-2xl font-semibold"
          />
        </div>
        <div className="border-l p-3">
          <div className="text-muted-foreground flex items-center justify-between text-xs">
            <span>并发</span>
            <span className="text-[10px]">{conc.custom ? "自定义" : "默认"}</span>
          </div>
          <div
            className={cn(
              "mt-1 font-mono text-2xl font-semibold tabular-nums",
              conc.full && "text-status-warning",
            )}
          >
            {conc.active}
            <span className="text-muted-foreground text-base">/{conc.limit}</span>
          </div>
          <span
            className="bg-muted mt-1.5 block h-1 w-full overflow-hidden rounded-full"
            aria-hidden
          >
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
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <ReasonTooltip reason={creditDeny}>
          <CreditPopover
            user={user}
            actions={actions}
            open={creditOpen}
            onOpenChange={setCreditOpen}
            align="start"
            render={<MotionButton variant="outline" disabled={!!creditDeny} />}
          >
            <Diff />
            调整积分
          </CreditPopover>
        </ReasonTooltip>
        <ReasonTooltip reason={limitDeny}>
          <LimitPopover
            user={user}
            actions={actions}
            open={limitOpen}
            onOpenChange={setLimitOpen}
            align="start"
            render={<MotionButton variant="outline" disabled={!!limitDeny} />}
          >
            <Gauge />
            并发上限
          </LimitPopover>
        </ReasonTooltip>
        <ReasonTooltip reason={banDeny}>
          <MotionButton
            variant="outline"
            disabled={!!banDeny || statusBusy}
            className={cn(!disabled && "text-destructive hover:text-destructive")}
            onClick={() => void actions.setStatus(user, disabled ? "active" : "disabled")}
          >
            {statusBusy ? (
              <Loader2 className="animate-spin" />
            ) : disabled ? (
              <CircleCheck />
            ) : (
              <Ban />
            )}
            {disabled ? "启用" : "封禁"}
          </MotionButton>
        </ReasonTooltip>
        <div className="ml-auto">
          <UserRowMenu user={user} actor={actor} actions={actions} />
        </div>
      </div>
    </div>
  );
}

export { UserDialog };
