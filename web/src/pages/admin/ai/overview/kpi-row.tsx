import { Boxes, CircleCheck, Clapperboard, RadioTower } from "lucide-react";

import type { AdminStats, AdminStatsDays } from "@/api/admin/ai/type";
import { AnimatedNumber } from "@/components/admin-ui/animated-number";
import { Reveal } from "@/components/admin-ui/reveal";
import {
  StatCard,
  StatCardHint,
  StatCardLabel,
  StatCardUnit,
  StatCardValue,
} from "@/components/admin-ui/stat-card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  modelHint,
  successRate,
  todayAndYesterday,
  type HintTone,
} from "@/utils/admin/overview-stats";

import type { LoadStatus } from "../../use-admin";

/** 嵌在总览主面板里的指标格：不要卡片自己的底和描边，大数字再放大一档 */
const CELL = "h-full rounded-2xl bg-transparent p-5 shadow-none ring-0 backdrop-blur-none";
const CELL_INTERACTIVE = "hover:bg-chrome-hover";
const VALUE = "text-4xl tracking-tight";

/** 一个卡里的数字与说明还没就绪时的占位 */
function KpiSkeleton() {
  return (
    <>
      <Skeleton className="h-3.5 w-16" />
      <Skeleton className="mt-2 h-10 w-24" />
      <Skeleton className="mt-1 h-3.5 w-20" />
    </>
  );
}

/**
 * 总览主面板顶部的四个关键指标格（外层的清透面板由页面给）。
 * - 今日任务、成功率来自统计接口，统计加载失败时只这两张显示“—”；在线模型、可用渠道来自配置清单，互不影响；
 * - 在线模型、可用渠道整卡可点，都去渠道与模型主从页；
 * - 状态色只出现在说明文字上：有不可用的模型显示 danger，有待配置的模型或没设 Key 的渠道显示 warning。
 * @param stats 当前范围的统计，加载中为 null
 * @param statsStatus 统计的加载状态
 * @param days 当前统计范围，成功率的标题要用
 * @param configReady 渠道、插件、模型清单都已加载
 * @param online 在线模型数
 * @param modelTotal 模型总数
 * @param brokenModels 已启用但实际不可用的模型数
 * @param pendingModels 待配置的模型数（还没选能力或协议）
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
  pendingModels,
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
  pendingModels: number;
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
  const dash = <span className="text-muted-foreground">—</span>;

  const modelCardHint = modelHint({
    total: modelTotal,
    online,
    broken: brokenModels,
    pending: pendingModels,
  });
  const channelHint: { tone: HintTone; text: string } = !channelTotal
    ? { tone: "neutral", text: "还没有渠道" }
    : noKeyChannels
      ? { tone: "warning", text: `${noKeyChannels} 个未设 Key` }
      : channelTotal - usableChannels
        ? { tone: "neutral", text: `${channelTotal - usableChannels} 个不可用` }
        : { tone: "neutral", text: "全部可用" };

  return (
    <section aria-label="关键指标" className="grid grid-cols-2 gap-1 p-2 lg:grid-cols-4">
      <Reveal index={1}>
        <StatCard className={CELL}>
          {!stats && !statsFailed ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <Clapperboard />
                今日任务
              </StatCardLabel>
              <StatCardValue className={VALUE}>
                {statsFailed ? dash : <AnimatedNumber value={today} />}
              </StatCardValue>
              <StatCardHint>
                {statsFailed ? "统计加载失败" : `昨日 ${yesterday.toLocaleString()}`}
              </StatCardHint>
            </>
          )}
        </StatCard>
      </Reveal>

      <Reveal index={2}>
        <StatCard className={CELL}>
          {!stats && !statsFailed ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <CircleCheck />
                成功率 · 近 {days} 天
              </StatCardLabel>
              <StatCardValue className={VALUE}>
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
            </>
          )}
        </StatCard>
      </Reveal>

      <Reveal index={3}>
        <StatCard className={cn(CELL, CELL_INTERACTIVE)} onClick={onOpenModels}>
          {!configReady ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <Boxes />
                在线模型
              </StatCardLabel>
              <StatCardValue className={VALUE}>
                <AnimatedNumber value={online} />
                <StatCardUnit>/ {modelTotal}</StatCardUnit>
              </StatCardValue>
              <StatCardHint tone={modelCardHint.tone}>{modelCardHint.text}</StatCardHint>
            </>
          )}
        </StatCard>
      </Reveal>

      <Reveal index={4}>
        <StatCard className={cn(CELL, CELL_INTERACTIVE)} onClick={onOpenChannels}>
          {!configReady ? (
            <KpiSkeleton />
          ) : (
            <>
              <StatCardLabel>
                <RadioTower />
                可用渠道
              </StatCardLabel>
              <StatCardValue className={VALUE}>
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
