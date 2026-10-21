import { ArrowDown, ArrowUp, Minus } from "lucide-react";

import type { AgentStatsGroup } from "@/api/admin/agent-run/type.d";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  COMPARE_METRIC_LABEL,
  buildVersionCards,
  formatCredits,
  formatRate,
  type CompareMetric,
  type MetricDelta,
  type VersionCardView,
} from "@/utils/admin/agent-run";
import { agoText } from "@/utils/admin/log-rules";
import { formatDurationMs } from "@/utils/admin/user-rules";

import type { StatsStatus } from "./use-agent-stats";

/** 涨跌小标：降低为好用绿色，升高为红色，差距不足 2 个百分点算持平 */
function DeltaChip({ delta }: { delta: MetricDelta }) {
  const label = COMPARE_METRIC_LABEL[delta.metric];
  if (delta.verdict === "flat")
    return (
      <span
        data-slot="delta-chip"
        data-verdict="flat"
        title={`${label}与上一版相差不到 2 个百分点，算持平`}
        className="text-muted-foreground inline-flex items-center gap-0.5 text-xs"
      >
        <Minus className="size-3" aria-hidden="true" />
        持平
      </span>
    );
  const better = delta.verdict === "better";
  const Icon = better ? ArrowDown : ArrowUp;
  return (
    <span
      data-slot="delta-chip"
      data-verdict={delta.verdict}
      title={`${label}比上一版${better ? "降低" : "升高"} ${Math.abs(delta.diffPp)} 个百分点（降低为好）`}
      className={cn(
        "inline-flex items-center gap-0.5 text-xs font-medium tabular-nums",
        better ? "text-status-success" : "text-destructive",
      )}
    >
      <Icon className="size-3" aria-hidden="true" />
      {Math.abs(delta.diffPp)} 个百分点
      <span className="sr-only">{better ? "，变好" : "，变差"}</span>
    </span>
  );
}

/** 一格指标：名称、数值、（可选）相对上一版的涨跌 */
function Metric({
  label,
  value,
  delta,
}: {
  label: string;
  value: string;
  delta?: MetricDelta | null;
}) {
  return (
    <div data-slot="version-metric" className="min-w-0">
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className="flex flex-wrap items-baseline gap-x-1.5">
        <span className="text-sm font-medium tabular-nums">{value}</span>
        {delta && <DeltaChip delta={delta} />}
      </div>
    </div>
  );
}

/** 一张版本卡片：整张可点，点了按该版本筛选列表，再点取消 */
function VersionCard({
  card,
  selected,
  now,
  onSelect,
}: {
  card: VersionCardView;
  selected: boolean;
  now: number;
  onSelect: (key: string) => void;
}) {
  const { group } = card;
  const delta = (metric: CompareMetric) => card.deltas?.find((d) => d.metric === metric) ?? null;
  return (
    <button
      type="button"
      data-slot="version-card"
      aria-pressed={selected}
      title={selected ? "再点一次取消筛选" : "只看这个版本的运行"}
      onClick={() => onSelect(group.key)}
      className={cn(
        "bg-card hover:bg-muted/40 focus-visible:ring-ring/50 flex min-w-0 flex-col gap-3 rounded-xl border p-4 text-left transition-colors outline-none focus-visible:ring-2",
        selected && "border-foreground/60 ring-foreground/20 ring-1",
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <b className="font-mono text-sm">{card.label}</b>
        {card.insufficient && (
          <Tag tone="warning" title="运行数不足 30，只显示数值，不给变好或变坏的结论">
            样本不足
          </Tag>
        )}
      </div>
      <div className="text-muted-foreground text-xs tabular-nums">
        {group.n} 次运行 · 最近 {agoText(group.last_at, now)}
      </div>
      <div className="grid grid-cols-3 gap-x-3 gap-y-2">
        <Metric label="点踩率" value={formatRate(group.down_rate)} delta={delta("down_rate")} />
        <Metric label="撤销率" value={formatRate(group.undo_rate)} delta={delta("undo_rate")} />
        <Metric
          label="失败结束率"
          value={formatRate(group.fail_end_rate)}
          delta={delta("fail_end_rate")}
        />
        <Metric label="中位耗时" value={formatDurationMs(group.median_duration_ms)} />
        <Metric label="平均积分" value={formatCredits(group.avg_credits)} />
        <Metric label="反馈率" value={formatRate(group.feedback_rate)} />
      </div>
    </button>
  );
}

/**
 * 版本对比卡片区：最近 3 个 harness 版本的核心指标。
 * 样本不足（n<30）的卡片只显示数值；相邻两张都够样本时，较新的一张显示相对较旧一张的点踩率、撤销率、失败结束率变化。
 * 加载失败只在这里提示并给重试，不影响下面的列表。
 * @param status 加载状态
 * @param groups 按 harness_key 分组的统计（最近运行的在前）
 * @param selectedKey 列表正按哪个 harness_key 筛选，空串表示没有
 * @param now 当前时间（毫秒），用来显示「最近运行」
 */
export function VersionCards({
  status,
  groups,
  selectedKey,
  now,
  onSelect,
  onRetry,
}: {
  status: StatsStatus;
  groups: AgentStatsGroup[];
  selectedKey: string;
  now: number;
  onSelect: (key: string) => void;
  onRetry: () => void;
}) {
  const cards = buildVersionCards(groups);
  return (
    <section data-slot="version-cards" aria-label="版本对比" className="mb-4">
      <div className="mb-2 flex items-baseline gap-2">
        <h2 className="text-sm font-semibold">版本对比</h2>
        <span className="text-muted-foreground text-xs">最近 30 天 · 点卡片按版本筛选列表</span>
      </div>
      {status === "loading" && (
        <div className="grid gap-3 md:grid-cols-3" aria-busy="true">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="h-36 rounded-xl" />
          ))}
        </div>
      )}
      {status === "error" && (
        <Notice
          tone="danger"
          title="版本对比加载失败"
          action={
            <Button variant="outline" size="sm" onClick={onRetry}>
              重试
            </Button>
          }
        />
      )}
      {status === "ready" && cards.length === 0 && (
        <div
          data-slot="version-cards-empty"
          className="text-muted-foreground rounded-xl border border-dashed p-6 text-center text-sm"
        >
          最近 30 天还没有运行记录，没有可对比的版本。
        </div>
      )}
      {status === "ready" && cards.length > 0 && (
        <div className="grid gap-3 md:grid-cols-3">
          {cards.map((card) => (
            <VersionCard
              key={card.group.key}
              card={card}
              selected={selectedKey === card.group.key}
              now={now}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </section>
  );
}
