import { MotionConfig } from "motion/react";
import { useState } from "react";

import { useNow } from "@/hooks/use-now";

import { AgentRunsView } from "./agent-runs-view";
import { RunDrawer } from "./run-drawer";
import { useAgentRuns } from "./use-agent-runs";
import { useAgentStats } from "./use-agent-stats";

/**
 * Agent 运行记录页（AI 配置 › 运行记录）：版本对比卡片 + 筛选栏 + 运行列表，点一行从右侧抽屉看时间线和详情。
 * 版本对比加载失败只在卡片区提示，不影响列表。详情抽屉只是页面状态，不写进 URL；
 * 关闭动画播完后才清掉所看的运行，退出过程中内容不会闪空。
 */
export default function AgentRunsPage() {
  const runs = useAgentRuns();
  const stats = useAgentStats();
  /** 每分钟刷新一次，卡片上的「最近运行」不用刷新页面就会更新 */
  const now = useNow(true, 60_000);
  const [drawer, setDrawer] = useState<{ rid: string | null; open: boolean }>({
    rid: null,
    open: false,
  });

  return (
    <MotionConfig reducedMotion="user">
      <AgentRunsView
        statsStatus={stats.status}
        harness={stats.harness}
        models={stats.models}
        filters={runs.filters}
        filtersActive={runs.active}
        listState={runs.list.state}
        items={runs.list.items}
        hasMore={runs.list.hasMore}
        loadingMore={runs.list.loadingMore}
        activeRid={drawer.open ? drawer.rid : null}
        now={now}
        onSelectHarness={runs.toggleHarness}
        onRetryStats={stats.retry}
        onFilterChange={runs.patch}
        onReset={runs.reset}
        onOpen={(rid) => setDrawer({ rid, open: true })}
        onMore={runs.list.more}
        onRetryList={runs.list.retry}
      />
      <RunDrawer
        open={drawer.open}
        rid={drawer.rid}
        onClose={() => setDrawer((prev) => ({ ...prev, open: false }))}
        onCleared={() => setDrawer((prev) => (prev.open ? prev : { rid: null, open: false }))}
      />
    </MotionConfig>
  );
}
