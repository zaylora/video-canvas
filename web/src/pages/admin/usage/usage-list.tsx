import { ChevronRight, FlaskConical } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import type { KeyboardEvent } from "react";

import type { UsageItem } from "@/api/admin/usage/type";
import { Tag } from "@/components/admin-ui/tag";
import { Skeleton } from "@/components/ui/skeleton";
import { useNow } from "@/hooks/use-now";
import { DURATION, EASE_OUT, STAGGER, STAGGER_MAX, TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  agoText,
  isActiveStatus,
  taskHeadline,
  taskSubline,
  usageStatusView,
} from "@/utils/admin/log-rules";
import { formatAbsolute } from "@/utils/admin/user-rules";

import { TONE_DOT } from "./tone";

/** 首次加载的卡片骨架：6 条，高度同渠道页 */
function UsageListSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-2" aria-busy>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-14 rounded-lg" />
      ))}
    </div>
  );
}

/**
 * 使用记录卡片列表（替换原来的表格）：一张卡一个任务。
 * 第一行：状态点 + 用户名（点击加入用户筛选）+ 一句话 +（试跑标签）+ 相对时间；
 * 第二行：模型 · 积分 · 用时 / 原因 / 已等多久。整卡可点（Enter / Space 同样）打开详情抽屉。
 * 样式沿用渠道页 ChannelItem：选中边框 primary/60，hover 底色变深。
 * @param activeRef 抽屉里正在看的任务，卡片高亮
 */
function UsageList({
  items,
  activeRef,
  onOpen,
  onFilterUser,
}: {
  items: UsageItem[];
  activeRef: string | null;
  onOpen: (ref: string) => void;
  onFilterUser: (user: UsageItem["user"]) => void;
}) {
  const reduced = useReducedMotion();
  const now = useNow(items.some((item) => isActiveStatus(item.status)));
  const onKey = (event: KeyboardEvent, ref: string) => {
    if (event.target !== event.currentTarget) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onOpen(ref);
    }
  };
  return (
    <div data-slot="usage-list" className="flex flex-col gap-2">
      {items.map((item, index) => {
        const status = usageStatusView(item.status);
        const selected = item.task_ref === activeRef;
        return (
          <motion.div
            key={item.id}
            role="button"
            tabIndex={0}
            aria-current={selected ? "true" : undefined}
            initial={reduced ? { opacity: 0 } : { opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{
              duration: DURATION.base,
              ease: EASE_OUT,
              delay: Math.min(index, STAGGER_MAX) * STAGGER,
            }}
            whileTap={reduced ? undefined : TAP_CARD}
            onClick={() => onOpen(item.task_ref)}
            onKeyDown={(event) => onKey(event, item.task_ref)}
            className={cn(
              "bg-card focus-visible:ring-ring/50 flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 outline-none transition-colors focus-visible:ring-3",
              selected ? "border-primary/60 bg-primary/5" : "hover:bg-muted/60",
            )}
          >
            <span
              aria-hidden
              className={cn(
                "mt-1.5 size-2 shrink-0 rounded-full",
                TONE_DOT[status.tone],
                status.active && "animate-pulse motion-reduce:animate-none",
              )}
            />
            <span className="min-w-0 flex-1">
              <span className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm">
                <button
                  type="button"
                  title="只看这个用户"
                  className="hover:text-primary font-medium underline-offset-2 hover:underline"
                  onClick={(event) => {
                    event.stopPropagation();
                    onFilterUser(item.user);
                  }}
                  onKeyDown={(event) => event.stopPropagation()}
                >
                  {item.user.username}
                </button>
                <span>{taskHeadline(item)}</span>
                {item.is_test && (
                  <Tag tone="violet">
                    <FlaskConical className="size-3" />
                    试跑
                  </Tag>
                )}
              </span>
              <span className="text-muted-foreground mt-0.5 block text-xs break-words">
                {taskSubline(item, now)}
              </span>
              <span
                className="text-muted-foreground mt-0.5 block text-xs sm:hidden"
                title={formatAbsolute(item.created_at)}
              >
                {agoText(item.created_at, now)}
              </span>
            </span>
            <span
              className="text-muted-foreground hidden shrink-0 pt-0.5 text-xs sm:block"
              title={formatAbsolute(item.created_at)}
            >
              {agoText(item.created_at, now)}
            </span>
            <ChevronRight className="text-muted-foreground mt-0.5 size-4 shrink-0" />
          </motion.div>
        );
      })}
    </div>
  );
}

export { UsageList, UsageListSkeleton };
