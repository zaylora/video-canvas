import { CircleCheck, LayoutList, Loader, TriangleAlert } from "lucide-react";

import type { UsageStatusFilter, UsageSummary } from "@/api/admin/usage/type";
import { AnimatedNumber } from "@/components/admin-ui/animated-number";
import {
  StatCard,
  StatCardHint,
  StatCardLabel,
  StatCardValue,
} from "@/components/admin-ui/stat-card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { overviewCards } from "@/utils/admin/log-rules";

const SELECTED = "ring-primary/60 bg-primary/5";

/** 副标题里可以单独点的数字（失败 / 超时 / 取消） */
function HintLink({
  label,
  count,
  onClick,
}: {
  label: string;
  count: number;
  onClick: () => void;
}) {
  if (count === 0) return <span>{`${label} ${count}`}</span>;
  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:text-foreground focus-visible:ring-ring/50 rounded-sm underline-offset-2 outline-none hover:underline focus-visible:ring-2"
    >
      {`${label} ${count}`}
    </button>
  );
}

/**
 * 概览 4 张卡（全部 / 成功 / 需要关注 / 进行中），点击 = 一键改状态筛选。
 * 需要关注点击落到「失败」（后端 status 只支持单值），副标题里的失败 / 超时 / 取消各自可点；
 * 大于 0 时副标题用 warning 色。当前筛选下为 0 的卡置灰且不可点；加载中显示骨架，失败不显示。
 * @param rangeLabel 当前时间范围名，写在「全部」的副标题
 */
function UsageOverview({
  summary,
  loading,
  status,
  rangeLabel,
  onStatus,
}: {
  summary: UsageSummary | null;
  loading: boolean;
  status: UsageStatusFilter;
  rangeLabel: string;
  onStatus: (status: UsageStatusFilter) => void;
}) {
  if (!summary) {
    return loading ? (
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-busy>
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-[92px] rounded-xl" />
        ))}
      </div>
    ) : null;
  }
  const o = overviewCards(summary);
  const attention = o.attention;
  return (
    <div data-slot="usage-overview" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <StatCard
        onClick={o.total > 0 ? () => onStatus("all") : undefined}
        className={cn(status === "all" && SELECTED, o.total === 0 && "opacity-50")}
      >
        <StatCardLabel>
          <LayoutList />
          全部
        </StatCardLabel>
        <StatCardValue>
          <AnimatedNumber value={o.total} />
        </StatCardValue>
        <StatCardHint>{rangeLabel}</StatCardHint>
      </StatCard>
      <StatCard
        onClick={o.succeeded > 0 ? () => onStatus("succeeded") : undefined}
        className={cn(status === "succeeded" && SELECTED, o.succeeded === 0 && "opacity-50")}
      >
        <StatCardLabel>
          <CircleCheck />
          成功
        </StatCardLabel>
        <StatCardValue>
          <AnimatedNumber value={o.succeeded} />
        </StatCardValue>
        <StatCardHint>成功率 {o.successRate}</StatCardHint>
      </StatCard>
      {/* 副标题里有可点的数字，整卡不能再是按钮（按钮不能嵌套），所以用静态卡 + 内部按钮 */}
      <StatCard
        className={cn(
          "hover:bg-muted/40 transition-colors",
          status === "failed" && SELECTED,
          attention.count === 0 && "opacity-50",
        )}
      >
        <button
          type="button"
          disabled={attention.count === 0}
          onClick={() => onStatus("failed")}
          className="focus-visible:ring-ring/50 flex flex-col gap-1 rounded-sm text-left outline-none focus-visible:ring-2 disabled:cursor-default"
        >
          <StatCardLabel>
            <TriangleAlert />
            需要关注
          </StatCardLabel>
          <StatCardValue>
            <AnimatedNumber value={attention.count} />
          </StatCardValue>
        </button>
        <StatCardHint
          tone={attention.count > 0 ? undefined : "neutral"}
          className={cn("flex flex-wrap gap-x-1.5", attention.count > 0 && "text-status-warning")}
        >
          <HintLink label="失败" count={attention.failed} onClick={() => onStatus("failed")} />
          <span aria-hidden>·</span>
          <HintLink label="超时" count={attention.expired} onClick={() => onStatus("expired")} />
          <span aria-hidden>·</span>
          <HintLink label="取消" count={attention.canceled} onClick={() => onStatus("canceled")} />
        </StatCardHint>
      </StatCard>
      <StatCard
        onClick={o.active > 0 ? () => onStatus("active") : undefined}
        className={cn(status === "active" && SELECTED, o.active === 0 && "opacity-50")}
      >
        <StatCardLabel>
          <Loader />
          进行中
        </StatCardLabel>
        <StatCardValue>
          <AnimatedNumber value={o.active} />
        </StatCardValue>
        <StatCardHint>现在正在生成</StatCardHint>
      </StatCard>
    </div>
  );
}

export { UsageOverview };
