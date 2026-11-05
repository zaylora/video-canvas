import { MotionConfig, AnimatePresence, motion } from "motion/react";
import { RefreshCw, TriangleAlert } from "lucide-react";
import { useRef, useState } from "react";
import { useNavigate } from "react-router";

import type { AdminStatsDays } from "@/api/admin/ai/type";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { GLASS_PANEL, GlassSegmented } from "@/components/admin-ui/glass";
import { Reveal } from "@/components/admin-ui/reveal";
import { toneClasses } from "@/components/admin-ui/tag";
import { ChromeButton, ChromePill, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { channelsPagePath } from "@/utils/admin/channel-route";
import { canManageInfra } from "@/utils/admin/role";

import { ChannelLoad } from "./channel-load";
import { KpiRow } from "./kpi-row";
import { OnboardingSteps, type OnboardingStep } from "./onboarding-steps";
import { ShareRanking } from "./share-ranking";
import { TaskTrendChart } from "./task-trend-chart";
import { TodoList } from "./todo-list";
import { useChannelLoads, useOverview, useOverviewStats } from "./use-overview-stats";

const RANGES: { days: AdminStatsDays; label: string }[] = [
  { days: 7, label: "近 7 天" },
  { days: 30, label: "近 30 天" },
];

/**
 * 总览页（后台首页）：一眼看清“系统健康吗、最近用得怎么样、哪里要处理”。
 * 设计稿见 docs/design/总览页。
 * 视觉是后台统一的「清透」风格（admin-ui/glass.tsx）：面板半透明 + 背景模糊，透出外壳的光晕。
 * - 上线引导：没走完时显示三步大卡，三步全绿后不再显示；
 * - 主面板：关键指标（今日任务、成功率、在线模型、可用渠道）+ 近 7 / 30 天任务量面积图；
 * - 下面一排：调用排行、渠道实时负载、待处理；
 * - 待处理：有事时页头出现“N 项待处理”，点击滚到清单并闪一下描边；
 * 配置类数据（含待处理清单的文案）由后端汇总好（GET /admin/ai/overview），前端只按待处理项的 action.kind 处理；
 * 运行类数据来自 GET /admin/ai/stats。点卡片、去渠道页都进渠道与模型主从页（channelsPagePath）。
 */
export default function OverviewPage() {
  const { overview, status: overviewStatus, reload: reloadOverview } = useOverview();
  const stats = useOverviewStats();
  const loads = useChannelLoads();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();

  const [refreshing, setRefreshing] = useState(false);
  const [flash, setFlash] = useState(0);
  const todoCardRef = useRef<HTMLDivElement>(null);

  // 配置类数据都由后端汇总好（GET /admin/ai/overview），没加载到时按 0 / 空处理，由 ready 控制骨架
  const ready = overviewStatus === "ready" && overview !== null;
  const usablePlugins = overview?.plugins.usable ?? 0;
  const channelTotal = overview?.channels.total ?? 0;
  const usableChannels = overview?.channels.usable ?? 0;
  const noKeyChannels = overview?.channels.no_key ?? 0;
  const modelTotal = overview?.models.total ?? 0;
  const online = overview?.models.online ?? 0;
  const brokenModels = overview?.models.broken ?? 0;
  const pendingModels = overview?.models.pending ?? 0;
  const todos = overview?.todos ?? [];

  const steps: OnboardingStep[] = [
    {
      title: "接入平台",
      ok: usablePlugins > 0,
      stat: `${usablePlugins} 个可用`,
      short: `${usablePlugins} 个插件`,
      action: canWrite ? { label: "上传插件", to: "/admin/ai/plugins" } : null,
    },
    {
      title: "配置渠道",
      ok: usableChannels > 0,
      stat: `${usableChannels} / ${channelTotal} 个可用`,
      short: `${usableChannels} / ${channelTotal} 可用`,
      action: canWrite ? { label: "新建渠道", to: channelsPagePath() } : null,
    },
    {
      title: "上线模型",
      ok: online > 0,
      stat: `${online} 个在线`,
      short: `${online} 个在线`,
      action: { label: "添加模型", to: channelsPagePath() },
    },
  ];

  /** 刷新全部：统计、渠道负载和配置汇总；各自的失败在对应卡片里显示，这里只管等它们结束 */
  const refresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([stats.reload(), loads.reload(), reloadOverview()]);
    } finally {
      setRefreshing(false);
    }
  };

  /** 点页头的“N 项待处理”：滚到清单并让描边闪一下 */
  const focusTodos = () => {
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    todoCardRef.current?.scrollIntoView({ behavior: reduced ? "auto" : "smooth", block: "center" });
    setFlash((n) => n + 1);
  };

  return (
    <MotionConfig reducedMotion="user">
      <div className="h-full overflow-y-auto">
        <main className="px-4 py-6 lg:px-6">
          <PageHeader>
            <PageHeaderHeading>
              <PageHeaderTitle>总览</PageHeaderTitle>
            </PageHeaderHeading>
            <PageHeaderActions>
              <AnimatePresence>
                {todos.length > 0 && (
                  <motion.button
                    type="button"
                    initial={{ opacity: 0, scale: 0.96 }}
                    animate={{
                      opacity: 1,
                      scale: 1,
                      transition: { duration: DURATION.base, ease: EASE_OUT },
                    }}
                    exit={{
                      opacity: 0,
                      scale: 0.96,
                      transition: { duration: DURATION.exit, ease: EASE_OUT },
                    }}
                    whileTap={TAP}
                    onClick={focusTodos}
                    className={cn(
                      toneClasses.warning,
                      "focus-visible:ring-ring inline-flex h-10 items-center gap-1.5 rounded-full px-4 text-sm font-medium focus-visible:ring-2 focus-visible:outline-none",
                    )}
                  >
                    <TriangleAlert className="size-4" />
                    <span className="tabular-nums">{todos.length}</span> 项待处理
                  </motion.button>
                )}
              </AnimatePresence>
              <GlassSegmented
                id="overview-range"
                label="统计范围"
                value={stats.days}
                onValueChange={stats.changeDays}
                items={RANGES.map(({ days, label }) => ({ value: days, label }))}
              />
              <ChromePill>
                <ChromeTooltip label="刷新" side="bottom">
                  <ChromeButton
                    aria-label="刷新"
                    disabled={refreshing}
                    onClick={() => void refresh()}
                  >
                    <RefreshCw className={cn(refreshing && "animate-spin")} />
                  </ChromeButton>
                </ChromeTooltip>
              </ChromePill>
            </PageHeaderActions>
          </PageHeader>

          <Reveal>
            <OnboardingSteps
              steps={steps}
              ready={ready}
              onNavigate={(to, state) => navigate(to, { state })}
            />
          </Reveal>

          <div className="grid gap-4">
            <Reveal index={1}>
              <section aria-label="任务概况" className={cn(GLASS_PANEL, "overflow-hidden")}>
                <KpiRow
                  stats={stats.stats}
                  statsStatus={stats.status}
                  days={stats.days}
                  configReady={ready}
                  online={online}
                  modelTotal={modelTotal}
                  brokenModels={brokenModels}
                  pendingModels={pendingModels}
                  usableChannels={usableChannels}
                  channelTotal={channelTotal}
                  noKeyChannels={noKeyChannels}
                  onOpenModels={() => navigate(channelsPagePath())}
                  onOpenChannels={() => navigate(channelsPagePath())}
                />
                <div aria-hidden className="bg-chrome-border mx-5 h-px" />
                <TaskTrendChart
                  stats={stats.stats}
                  status={stats.status}
                  onRetry={() => void stats.reload()}
                  className="rounded-none bg-transparent py-5 ring-0"
                />
              </section>
            </Reveal>

            <section aria-label="调用与渠道" className="grid gap-4 lg:grid-cols-3">
              <Reveal index={2} className="flex min-w-0 flex-col [&>*]:flex-1">
                <ShareRanking
                  stats={stats.stats}
                  status={stats.status}
                  onRetry={() => void stats.reload()}
                />
              </Reveal>
              <Reveal index={3} className="min-w-0">
                <ChannelLoad
                  channels={overview?.channel_briefs ?? []}
                  catalogReady={ready}
                  loads={loads.loads}
                  status={loads.status}
                  stale={loads.stale}
                  onRetry={() => void loads.reload()}
                  onOpenChannels={() => navigate(channelsPagePath())}
                  onCreateChannel={canWrite ? () => navigate(channelsPagePath()) : null}
                />
              </Reveal>
              <Reveal index={4} className="min-w-0">
                <div ref={todoCardRef} className="h-full">
                  <TodoList
                    todos={todos}
                    ready={ready}
                    canWrite={canWrite}
                    onChanged={() => void reloadOverview()}
                    flash={flash}
                  />
                </div>
              </Reveal>
            </section>
          </div>
        </main>
      </div>
    </MotionConfig>
  );
}
