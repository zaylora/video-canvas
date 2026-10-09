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
import { Reveal } from "@/components/admin-ui/reveal";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { toneClasses } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { adminTodos, channelHealth, modelHealth } from "@/utils/admin/health";
import { canManageInfra } from "@/utils/admin/role";

import { useAdminOutlet, useModelList } from "../../use-admin";
import { ChannelLoad } from "./channel-load";
import { KpiRow } from "./kpi-row";
import { OnboardingSteps, type OnboardingStep } from "./onboarding-steps";
import { ShareDonut } from "./share-donut";
import { TaskTrendChart } from "./task-trend-chart";
import { TodoList } from "./todo-list";
import { useChannelLoads, useOverviewStats } from "./use-overview-stats";

const RANGES: { days: AdminStatsDays; label: string }[] = [
  { days: 7, label: "近 7 天" },
  { days: 30, label: "近 30 天" },
];

/**
 * 总览页（后台首页）：一眼看清“系统健康吗、最近用得怎么样、哪里要处理”。
 * 设计稿见 docs/design/总览页。
 * - 上线引导：三步全绿后收成一行细条，没走完时展开成大卡；
 * - 关键指标：今日任务、成功率（统计接口）与在线模型、可用渠道（配置清单）；
 * - 图表：近 7 / 30 天任务量（堆叠柱形图）、调用占比（环形图）、渠道实时负载；
 * - 待处理：有事时页头出现“N 项待处理”，点击滚到清单并闪一下描边；
 * 配置类数据来自现有清单接口、在前端计算（utils/admin/health.ts），运行类数据来自 GET /admin/ai/stats。
 */
export default function OverviewPage() {
  const { catalog } = useAdminOutlet();
  const { models, status: modelsStatus, reload: reloadModels } = useModelList();
  const stats = useOverviewStats();
  const loads = useChannelLoads();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();

  const [refreshing, setRefreshing] = useState(false);
  const [flash, setFlash] = useState(0);
  const todoCardRef = useRef<HTMLDivElement>(null);

  const ready =
    catalog.pluginsStatus === "ready" &&
    catalog.channelsStatus === "ready" &&
    modelsStatus === "ready";
  const catalogReady = catalog.pluginsStatus === "ready" && catalog.channelsStatus === "ready";
  const usablePlugins = catalog.plugins.filter((item) => item.enabled).length;
  const channelStates = catalog.channels.map((item) => channelHealth(item, catalog.plugins));
  const usableChannels = channelStates.filter((item) => item.tone === "ok").length;
  const noKeyChannels = channelStates.filter((item) => item.label === "未设 Key").length;
  const modelStates = models.map((item) => modelHealth(item, catalog.channels, catalog.plugins));
  const online = modelStates.filter((item) => item.status === "online").length;
  const brokenModels = modelStates.filter((item) => item.status === "broken").length;
  const todos = ready ? adminTodos(models, catalog.channels, catalog.plugins) : [];

  const steps: OnboardingStep[] = [
    {
      title: "接入平台",
      hint: "插件",
      ok: usablePlugins > 0,
      stat: `${usablePlugins} 个可用`,
      short: `${usablePlugins} 个插件`,
      action: canWrite ? { label: "上传插件", to: "/admin/ai/plugins" } : null,
    },
    {
      title: "配置渠道",
      hint: "地址 + Key",
      ok: usableChannels > 0,
      stat: `${usableChannels} / ${catalog.channels.length} 个可用`,
      short: `${usableChannels} / ${catalog.channels.length} 可用`,
      action: canWrite ? { label: "新建渠道", to: "/admin/ai/channels?edit=new" } : null,
    },
    {
      title: "上线模型",
      hint: "用户能在画布里选到",
      ok: online > 0,
      stat: `${online} 个在线`,
      short: `${online} 个在线`,
      action: { label: "新建模型", to: "/admin/ai/models/new" },
    },
  ];

  /** 刷新全部：统计、渠道负载和三份配置清单；各自的失败在对应卡片里显示，这里只管等它们结束 */
  const refresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([
        stats.reload(),
        loads.reload(),
        catalog.reloadPlugins(),
        catalog.reloadChannels(),
        reloadModels(),
      ]);
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
                      "focus-visible:ring-ring inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-sm font-medium focus-visible:ring-2 focus-visible:outline-none",
                    )}
                  >
                    <TriangleAlert className="size-4" />
                    <span className="tabular-nums">{todos.length}</span> 项待处理
                  </motion.button>
                )}
              </AnimatePresence>
              <Segmented aria-label="统计范围">
                {RANGES.map(({ days, label }) => (
                  <SegmentedItem
                    key={days}
                    slideId="overview-range"
                    active={stats.days === days}
                    onClick={() => stats.changeDays(days)}
                  >
                    {label}
                  </SegmentedItem>
                ))}
              </Segmented>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="outline"
                      size="icon"
                      aria-label="刷新"
                      disabled={refreshing}
                      onClick={() => void refresh()}
                    />
                  }
                >
                  <RefreshCw className={cn(refreshing && "animate-spin")} />
                </TooltipTrigger>
                <TooltipContent>刷新</TooltipContent>
              </Tooltip>
            </PageHeaderActions>
          </PageHeader>

          <Reveal>
            <OnboardingSteps steps={steps} ready={ready} onNavigate={navigate} />
          </Reveal>

          <div className="grid gap-3">
            <KpiRow
              stats={stats.stats}
              statsStatus={stats.status}
              days={stats.days}
              configReady={ready}
              online={online}
              modelTotal={models.length}
              brokenModels={brokenModels}
              usableChannels={usableChannels}
              channelTotal={catalog.channels.length}
              noKeyChannels={noKeyChannels}
              onOpenModels={() => navigate("/admin/ai/models")}
              onOpenChannels={() => navigate("/admin/ai/channels")}
            />

            <section
              aria-label="任务趋势"
              className="grid gap-3 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]"
            >
              <Reveal index={5} className="flex min-w-0 flex-col [&>*]:flex-1">
                <TaskTrendChart
                  stats={stats.stats}
                  status={stats.status}
                  onRetry={() => void stats.reload()}
                />
              </Reveal>
              <Reveal index={6} className="flex min-w-0 flex-col [&>*]:flex-1">
                <ShareDonut
                  stats={stats.stats}
                  status={stats.status}
                  onRetry={() => void stats.reload()}
                />
              </Reveal>
            </section>

            <section
              aria-label="渠道与待处理"
              className="grid gap-3 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]"
            >
              <Reveal index={7} className="min-w-0">
                <ChannelLoad
                  channels={catalog.channels}
                  plugins={catalog.plugins}
                  catalogReady={catalogReady}
                  loads={loads.loads}
                  status={loads.status}
                  stale={loads.stale}
                  onRetry={() => void loads.reload()}
                  onOpenChannels={() => navigate("/admin/ai/channels")}
                  onCreateChannel={canWrite ? () => navigate("/admin/ai/channels?edit=new") : null}
                />
              </Reveal>
              <Reveal index={8} className="min-w-0">
                <div ref={todoCardRef} className="h-full">
                  <TodoList
                    todos={todos}
                    ready={ready}
                    canWrite={canWrite}
                    catalog={catalog}
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
