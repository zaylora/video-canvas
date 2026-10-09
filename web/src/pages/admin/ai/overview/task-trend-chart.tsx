import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { BarShapeProps, TooltipContentProps } from "recharts";

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
import {
  dayTotal,
  formatMonthDay,
  formatTooltipDate,
  niceTicks,
  thinTicks,
} from "@/utils/admin/overview-stats";

import type { LoadStatus } from "../../use-admin";
import { ChartError } from "./chart-states";

/** 堆叠顺序：从下到上。失败放在最上面最显眼 */
const SERIES = [
  { key: "succeeded", label: "成功" },
  { key: "other", label: "其他" },
  { key: "failed", label: "失败" },
] as const;
type SeriesKey = (typeof SERIES)[number]["key"];

/** 颜色全走 token：成功 / 失败是状态色，“其他”（取消与进行中）用中性色 */
const chartConfig = {
  succeeded: { label: "成功", color: "var(--status-success)" },
  other: { label: "其他", color: "var(--chart-other)" },
  failed: { label: "失败", color: "var(--destructive)" },
} satisfies ChartConfig;

/**
 * 一段柱子的外形：只有这一天最上面那一段带圆角，段与段之间用 1px 卡片底色隔开。
 * recharts 的 radius 会给每一段都加圆角，叠起来中间就有缺口，所以自己画。
 * @param key 这一段属于哪个系列
 */
function stackShape(key: SeriesKey) {
  return function StackShape({ x, y, width, height, payload }: BarShapeProps) {
    if (!(height > 0)) return <g />;
    const day = payload as AdminStatsDay;
    const above = key === "succeeded" ? day.other + day.failed : key === "other" ? day.failed : 0;
    const r = above > 0 ? 0 : Math.min(width > 24 ? 4 : 2.5, height, width / 2);
    const d = `M${x},${y + height}V${y + r}${r ? `Q${x},${y} ${x + r},${y}` : ""}H${x + width - r}${
      r ? `Q${x + width},${y} ${x + width},${y + r}` : ""
    }V${y + height}Z`;
    return <path d={d} fill={`var(--color-${key})`} stroke="var(--card)" strokeWidth={1} />;
  };
}
const SHAPES = {
  succeeded: stackShape("succeeded"),
  other: stackShape("other"),
  failed: stackShape("failed"),
};

/** 悬停浮层：日期 + 三个桶 + 合计 */
function TrendTooltip({ active, payload }: TooltipContentProps) {
  const day = payload?.[0]?.payload as AdminStatsDay | undefined;
  if (!active || !day) return null;
  return (
    <div className="bg-card ring-foreground/10 min-w-34 rounded-lg px-2.5 py-2 text-xs shadow-xl ring-1">
      <div className="mb-1 font-medium">{formatTooltipDate(day.date)}</div>
      <div className="grid gap-1">
        {SERIES.map(({ key, label }) => (
          <div key={key} className="flex items-center gap-1.5">
            <span
              className="size-2 shrink-0 rounded-[2px]"
              style={{ background: `var(--color-${key})` }}
            />
            <span className="text-muted-foreground">{label}</span>
            <span className="ml-auto pl-4 font-medium tabular-nums">{day[key]}</span>
          </div>
        ))}
        <div className="border-border text-muted-foreground flex items-center border-t pt-1">
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
 * 任务量（堆叠柱形图）：每天一根柱，成功 / 其他 / 失败三段，对应统计口径里的三个桶。
 * - 柱子入场的“长起来”由 index.css 的 chart-bars-rise 负责（recharts 自带动画关掉）；
 *   图表按范围加 key，切 7 / 30 天时重新挂载、重播入场，定时刷新同范围的数据不重播；
 * - X 轴高度固定 24，与 index.css 里动画原点的 24px 对应，改这里要一起改；
 * - 一个任务都没有时保留坐标轴与网格，中间写提示。
 * @param stats 当前范围的统计，加载中为 null
 * @param status 统计的加载状态
 * @param onRetry 失败后的重试
 */
export function TaskTrendChart({
  stats,
  status,
  onRetry,
}: {
  stats: AdminStats | null;
  status: LoadStatus;
  onRetry: () => void;
}) {
  const total = useMemo(
    () => stats?.daily.reduce((sum, day) => sum + dayTotal(day), 0) ?? 0,
    [stats],
  );
  const axis = useMemo(
    () => niceTicks(Math.max(0, ...(stats?.daily.map(dayTotal) ?? []))),
    [stats],
  );
  const dates = useMemo(() => stats?.daily.map((day) => day.date) ?? [], [stats]);
  const today = dates.at(-1);

  return (
    <Card data-slot="task-trend-chart">
      <CardHeader>
        <CardTitle>任务量</CardTitle>
        <CardDescription className="text-xs tabular-nums">
          {stats
            ? total === 0
              ? `近 ${stats.days} 天`
              : `近 ${stats.days} 天共 ${total.toLocaleString()} 次 · 日均 ${Math.round(total / stats.days).toLocaleString()}`
            : " "}
        </CardDescription>
        <CardAction>
          <ul className="text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs" aria-hidden>
            {SERIES.map(({ key, label }) => (
              <li key={key} className="flex items-center gap-1.5">
                <span
                  className="size-2 rounded-[2px]"
                  style={{ background: `var(--color-${key}, ${chartConfig[key].color})` }}
                />
                {label}
              </li>
            ))}
          </ul>
        </CardAction>
      </CardHeader>
      <CardContent className="flex min-h-60 flex-1 flex-col">
        {status === "error" ? (
          <ChartError title="任务统计加载失败" onRetry={onRetry} />
        ) : !stats ? (
          <Skeleton className="min-h-60 flex-1" />
        ) : (
          <div className="relative flex min-h-60 flex-1">
            <ChartContainer
              key={stats.days}
              config={chartConfig}
              role="img"
              aria-label={`近 ${stats.days} 天任务量柱形图，按成功、其他、失败堆叠`}
              className="chart-bars-rise aspect-auto h-full min-h-60 w-full justify-start"
            >
              <BarChart data={stats.daily} margin={{ top: 8, right: 4, bottom: 0, left: 0 }}>
                <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />
                <XAxis
                  dataKey="date"
                  ticks={thinTicks(dates)}
                  interval={0}
                  tickFormatter={(date: string) => (date === today ? "今天" : formatMonthDay(date))}
                  tickLine={false}
                  axisLine={false}
                  tickMargin={6}
                  height={24}
                />
                <YAxis
                  width={34}
                  domain={[0, axis.max]}
                  ticks={axis.ticks}
                  allowDecimals={false}
                  tickLine={false}
                  axisLine={false}
                  tickMargin={4}
                />
                <ChartTooltip
                  cursor={{ fill: "var(--muted)", opacity: 0.55, radius: 6 }}
                  isAnimationActive={false}
                  content={TrendTooltip}
                />
                {SERIES.map(({ key }) => (
                  <Bar
                    key={key}
                    dataKey={key}
                    stackId="tasks"
                    fill={`var(--color-${key})`}
                    shape={SHAPES[key]}
                    maxBarSize={stats.days <= 7 ? 40 : 18}
                    isAnimationActive={false}
                  />
                ))}
              </BarChart>
            </ChartContainer>
            {total === 0 && (
              <div className="text-muted-foreground pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-1 pb-6 text-sm">
                这段时间还没有任务
                <span className="text-xs">有人在画布里生成后，这里就会出现柱子</span>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
