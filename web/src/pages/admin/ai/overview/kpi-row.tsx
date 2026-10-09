import { Boxes, CircleCheck, Clapperboard, RadioTower } from "lucide-react";
import { Bar, BarChart, Line, LineChart, YAxis } from "recharts";
import type { BarShapeProps } from "recharts";

import type { AdminStats, AdminStatsDays } from "@/api/admin/ai/type";
import { AnimatedNumber } from "@/components/admin-ui/animated-number";
import { Reveal } from "@/components/admin-ui/reveal";
import {
  StatCard,
  StatCardHint,
  StatCardLabel,
  StatCardSpark,
  StatCardUnit,
  StatCardValue,
} from "@/components/admin-ui/stat-card";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { dayTotal, successRate, todayAndYesterday } from "@/utils/admin/overview-stats";

import type { LoadStatus } from "../../use-admin";

/** 说明文字的语气 */
type HintTone = "neutral" | "warning" | "danger";

const taskSparkConfig = { total: { color: "var(--chart-1)" } } satisfies ChartConfig;
const rateSparkConfig = { rate: { color: "var(--status-success)" } } satisfies ChartConfig;

/** 迷你图固定取最近 7 天，最后一根（今天）的下标 */
const SPARK_LAST = 6;

/** 迷你柱：最后一根（今天）不透明，其余压到 45% */
function SparkBar({ x, y, width, height, index }: BarShapeProps) {
  return (
    <rect
      x={x}
      y={y}
      width={width}
      height={Math.max(height, 0)}
      rx={1.5}
      fill="var(--color-total)"
      opacity={index === SPARK_LAST ? 1 : 0.45}
    />
  );
}

/** 一个卡里的数字与说明还没就绪时的占位 */
function KpiSkeleton() {
  return (
    <>
      <Skeleton className="h-3.5 w-16" />
      <Skeleton className="mt-1.5 h-8 w-24" />
      <Skeleton className="mt-1 h-3.5 w-20" />
    </>
  );
}

/**
 * 总览顶部的四张关键指标卡。
 * - 今日任务、成功率来自统计接口，统计加载失败时只这两张显示“—”；在线模型、可用渠道来自配置清单，互不影响；
 * - 在线模型、可用渠道整卡可点，分别去模型页、渠道页；
 * - 状态色只出现在说明文字上：有不可用的模型显示 danger，有渠道没设 Key 显示 warning。
 * @param stats 当前范围的统计，加载中为 null
 * @param statsStatus 统计的加载状态
 * @param days 当前统计范围，成功率的标题要用
 * @param configReady 渠道、插件、模型清单都已加载
 * @param online 在线模型数
 * @param modelTotal 模型总数
 * @param brokenModels 已启用但实际不可用的模型数
 * @param usableChannels 可用渠道数
 * @param channelTotal 渠道总数
 * @param noKeyChannels 还没设置 Key 的渠道数
 * @param onOpenModels 点在线模型卡
 * @param onOpenChannels 点可用渠道卡
 */
export function KpiRow({
  stats,
  statsStatus,
  days,
  configReady,
  online,
  modelTotal,
  brokenModels,
  usableChannels,
  channelTotal,
  noKeyChannels,
  onOpenModels,
  onOpenChannels,
}: {
  stats: AdminStats | null;
  statsStatus: LoadStatus;
  days: AdminStatsDays;
  configReady: boolean;
  online: number;
  modelTotal: number;
  brokenModels: number;
  usableChannels: number;
  channelTotal: number;
  noKeyChannels: number;
  onOpenModels: () => void;
  onOpenChannels: () => void;
}) {
  const statsFailed = statsStatus === "error";
  const daily = stats?.daily ?? [];
  const { today, yesterday } = todayAndYesterday(daily);
  const rate = successRate(daily);
  const failed = daily.reduce((sum, day) => sum + day.failed, 0);
  const recent = daily.slice(-7);
  const rateSeries = recent.map((day) => ({
    rate:
      day.succeeded + day.failed > 0 ? (day.succeeded / (day.succeeded + day.failed)) * 100 : null,
  }));
  const idle = !stats || daily.every((day) => dayTotal(day) === 0);
  const dash = <span className="text-muted-foreground">—</span>;

  const modelHint: { tone: HintTone; text: string } = !modelTotal
    ? { tone: "neutral", text: "还没有模型" }
    : brokenModels
      ? { tone: "danger", text: `${brokenModels} 个不可用` }
      : modelTotal - online
        ? { tone: "neutral", text: `${modelTotal - online} 个未上线` }
        : { tone: "neutral", text: "全部在线" };
  const channelHint: { tone: HintTone; text: string } = !channelTotal
    ? { tone: "neutral", text: "还没有渠道" }
    : noKeyChannels
      ? { tone: "warning", text: `${noKeyChannels} 个未设 Key` }
      : channelTotal - usableChannels
        ? { tone: "neutral", text: `${channelTotal - usableChannels} 个不可用` }
        : { tone: "neutral", text: "全部可用" };

  return (
    <section aria-label="关键指标" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Reveal index={1}>
        <StatCard className="h-full">
          {!stats && !statsFailed ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <Clapperboard />
                今日任务
              </StatCardLabel>
              <StatCardValue>{statsFailed ? dash : <AnimatedNumber value={today} />}</StatCardValue>
              <StatCardHint>
                {statsFailed ? "统计加载失败" : `昨日 ${yesterday.toLocaleString()}`}
              </StatCardHint>
              {!statsFailed && !idle && (
                <StatCardSpark>
                  <ChartContainer config={taskSparkConfig} className="aspect-auto h-8 w-19">
                    <BarChart
                      data={recent.map((day) => ({ total: dayTotal(day) }))}
                      margin={{ top: 0, right: 0, bottom: 0, left: 0 }}
                      barCategoryGap={3}
                    >
                      <YAxis hide domain={[0, "dataMax"]} />
                      <Bar dataKey="total" shape={SparkBar} isAnimationActive={false} />
                    </BarChart>
                  </ChartContainer>
                </StatCardSpark>
              )}
            </>
          )}
        </StatCard>
      </Reveal>

      <Reveal index={2}>
        <StatCard className="h-full">
          {!stats && !statsFailed ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <CircleCheck />
                成功率 · 近 {days} 天
              </StatCardLabel>
              <StatCardValue>
                {statsFailed || rate === null ? (
                  dash
                ) : (
                  <>
                    <AnimatedNumber
                      value={rate}
                      format={{ minimumFractionDigits: 1, maximumFractionDigits: 1 }}
                    />
                    <StatCardUnit>%</StatCardUnit>
                  </>
                )}
              </StatCardValue>
              <StatCardHint>
                {statsFailed
                  ? "统计加载失败"
                  : rate === null
                    ? "还没有已结束的任务"
                    : `失败 ${failed.toLocaleString()} 次`}
              </StatCardHint>
              {!statsFailed &&
                rate !== null &&
                rateSeries.filter((p) => p.rate !== null).length > 1 && (
                  <StatCardSpark>
                    <ChartContainer config={rateSparkConfig} className="aspect-auto h-8 w-19">
                      <LineChart
                        data={rateSeries}
                        margin={{ top: 3, right: 2, bottom: 3, left: 2 }}
                      >
                        <YAxis hide domain={["dataMin", "dataMax"]} />
                        <Line
                          dataKey="rate"
                          type="monotone"
                          stroke="var(--color-rate)"
                          strokeWidth={2}
                          dot={false}
                          connectNulls
                          isAnimationActive={false}
                        />
                      </LineChart>
                    </ChartContainer>
                  </StatCardSpark>
                )}
            </>
          )}
        </StatCard>
      </Reveal>

      <Reveal index={3}>
        <StatCard className="h-full" onClick={onOpenModels}>
          {!configReady ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <Boxes />
                在线模型
              </StatCardLabel>
              <StatCardValue>
                <AnimatedNumber value={online} />
                <StatCardUnit>/ {modelTotal}</StatCardUnit>
              </StatCardValue>
              <StatCardHint tone={modelHint.tone}>{modelHint.text}</StatCardHint>
            </>
          )}
        </StatCard>
      </Reveal>

      <Reveal index={4}>
        <StatCard className="h-full" onClick={onOpenChannels}>
          {!configReady ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <RadioTower />
                可用渠道
              </StatCardLabel>
              <StatCardValue>
                <AnimatedNumber value={usableChannels} />
                <StatCardUnit>/ {channelTotal}</StatCardUnit>
              </StatCardValue>
              <StatCardHint tone={channelHint.tone}>{channelHint.text}</StatCardHint>
            </>
          )}
        </StatCard>
      </Reveal>
    </section>
  );
}
