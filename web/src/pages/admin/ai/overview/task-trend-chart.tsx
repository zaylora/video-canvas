import { useReducedMotion } from "motion/react";
import { useMemo } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { TooltipContentProps } from "recharts";

import type { AdminStats, AdminStatsDay } from "@/api/admin/ai/type";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  dayTotal,
  formatMonthDay,
  formatTooltipDate,
  niceTicks,
  thinTicks,
} from "@/utils/admin/overview-stats";

import type { LoadStatus } from "../../use-admin";
import { ChartError } from "./chart-states";

/** 浮层里逐项列出的三个桶 */
const BUCKETS = [
  { key: "succeeded", label: "成功", color: "var(--status-success)" },
  { key: "other", label: "其他", color: "var(--chart-other)" },
  { key: "failed", label: "失败", color: "var(--destructive)" },
] as const;

/** 图上只画两层：全部任务（底）与其中失败的（叠在上面），颜色全走 token */
const chartConfig = {
  total: { label: "全部", color: "var(--chart-1)" },
  failed: { label: "失败", color: "var(--destructive)" },
} satisfies ChartConfig;
const LAYERS = ["total", "failed"] as const;

/** 悬停浮层：日期 + 三个桶 + 合计 */
function TrendTooltip({ active, payload }: TooltipContentProps) {
  const day = payload?.[0]?.payload as AdminStatsDay | undefined;
  if (!active || !day) return null;
  return (
    <div className="bg-chrome ring-chrome-border min-w-34 rounded-2xl px-3 py-2 text-xs shadow-xl ring-1 backdrop-blur-xl">
      <div className="mb-1 font-medium">{formatTooltipDate(day.date)}</div>
      <div className="grid gap-1">
        {BUCKETS.map(({ key, label, color }) => (
          <div key={key} className="flex items-center gap-1.5">
            <span className="size-2 shrink-0 rounded-full" style={{ background: color }} />
            <span className="text-muted-foreground">{label}</span>
            <span className="ml-auto pl-4 font-medium tabular-nums">{day[key]}</span>
          </div>
        ))}
        <div className="border-chrome-border text-muted-foreground flex items-center border-t pt-1">
          合计
          <span className="text-foreground ml-auto pl-4 font-medium tabular-nums">
            {dayTotal(day)}
          </span>
        </div>
      </div>
    </div>
  );
}

/**
 * 任务量（面积图）：每天的全部任务一层，其中失败的一层叠在上面；悬停看成功 / 其他 / 失败三个桶。
 * - 曲线用 monotone，数据稀疏时也是一道平缓的坡，不会像柱形图那样留一大片空白；
 * - 入场用 recharts 自带的从左往右展开，开了“减少动态效果”就关掉；
 * - 一个任务都没有时保留坐标轴与网格，中间写提示。
 * @param stats 当前范围的统计，加载中为 null
 * @param status 统计的加载状态
 * @param onRetry 失败后的重试
 */
export function TaskTrendChart({
  stats,
  status,
  onRetry,
  className,
}: {
  stats: AdminStats | null;
  status: LoadStatus;
  onRetry: () => void;
  className?: string;
}) {
  const reduced = useReducedMotion();
  const data = useMemo(
    () => stats?.daily.map((day) => ({ ...day, total: dayTotal(day) })) ?? [],
    [stats],
  );
  const total = data.reduce((sum, day) => sum + day.total, 0);
  const axis = useMemo(() => niceTicks(Math.max(0, ...data.map((day) => day.total))), [data]);
  const dates = data.map((day) => day.date);
  const today = dates.at(-1);

  return (
    <Card data-slot="task-trend-chart" className={cn("gap-3", className)}>
      <CardHeader className="px-5">
        <CardTitle>任务量</CardTitle>
        <CardDescription className="text-xs tabular-nums">
          {stats
            ? total === 0
              ? `近 ${stats.days} 天`
              : `近 ${stats.days} 天共 ${total.toLocaleString()} 次 · 日均 ${Math.round(total / stats.days).toLocaleString()}`
            : " "}
        </CardDescription>
        <CardAction>
          <ul className="text-muted-foreground flex gap-3 text-xs" aria-hidden>
            {LAYERS.map((key) => (
              <li key={key} className="flex items-center gap-1.5">
                <span
                  className="size-2 rounded-full"
                  style={{ background: chartConfig[key].color }}
                />
                {chartConfig[key].label}
              </li>
            ))}
          </ul>
        </CardAction>
      </CardHeader>
      <CardContent className="flex min-h-56 flex-1 flex-col px-3">
        {status === "error" ? (
          <ChartError title="任务统计加载失败" onRetry={onRetry} />
        ) : !stats ? (
          <Skeleton className="mx-2 min-h-56 flex-1 rounded-2xl" />
        ) : (
          <div className="relative flex min-h-56 flex-1">
            <ChartContainer
              key={stats.days}
              config={chartConfig}
              role="img"
              aria-label={`近 ${stats.days} 天任务量面积图，含其中失败的任务`}
              className="aspect-auto h-full min-h-56 w-full justify-start"
            >
              <AreaChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
                <defs>
                  {LAYERS.map((key) => (
                    <linearGradient key={key} id={`trend-${key}`} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={`var(--color-${key})`} stopOpacity={0.35} />
                      <stop offset="100%" stopColor={`var(--color-${key})`} stopOpacity={0} />
                    </linearGradient>
                  ))}
                </defs>
                <CartesianGrid vertical={false} stroke="var(--chrome-border)" />
                <XAxis
                  dataKey="date"
                  ticks={thinTicks(dates)}
                  interval={0}
                  tickFormatter={(date: string) => (date === today ? "今天" : formatMonthDay(date))}
                  tickLine={false}
                  axisLine={false}
                  tickMargin={8}
                  padding={{ left: 12, right: 0 }}
                />
                <YAxis
                  width={36}
                  domain={[0, axis.max]}
                  ticks={axis.ticks}
                  allowDecimals={false}
                  tickLine={false}
                  axisLine={false}
                  tickMargin={4}
                />
                <ChartTooltip
                  cursor={{ stroke: "var(--chrome-border)", strokeWidth: 1 }}
                  isAnimationActive={false}
                  content={TrendTooltip}
                />
                {LAYERS.map((key) => (
                  <Area
                    key={key}
                    dataKey={key}
                    type="monotone"
                    stroke={`var(--color-${key})`}
                    strokeWidth={2}
                    fill={`url(#trend-${key})`}
                    activeDot={{ r: 4, strokeWidth: 2, stroke: "var(--background)" }}
                    isAnimationActive={!reduced}
                    animationDuration={600}
                  />
                ))}
              </AreaChart>
            </ChartContainer>
            {total === 0 && (
              <div className="text-muted-foreground pointer-events-none absolute inset-0 grid place-content-center pb-6 text-sm">
                这段时间还没有任务
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
