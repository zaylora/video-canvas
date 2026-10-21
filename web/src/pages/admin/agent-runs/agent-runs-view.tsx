import type { AgentRunDigest, AgentStatsGroup } from "@/api/admin/agent-run/type.d";
import { DataPage, DataPanel, DataPanelPagination } from "@/components/admin-ui/data-panel";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import type { RunFilters } from "@/utils/admin/agent-run";

import { RunFilterBar } from "./run-filter-bar";
import { RunList, type RunListState } from "./run-list";
import type { StatsStatus } from "./use-agent-stats";
import { VersionCards } from "./version-cards";

/** 运行记录页的全部输入：状态和回调都由外面给，页面本身没有隐藏状态，便于单独验证 */
export type AgentRunsViewProps = {
  /** 版本对比的加载状态 */
  statsStatus: StatsStatus;
  /** 按 harness_key 分组的统计（版本卡片与 Harness 下拉共用） */
  harness: AgentStatsGroup[];
  /** 按 model_key 分组的统计（模型下拉） */
  models: AgentStatsGroup[];
  /** 当前筛选 */
  filters: RunFilters;
  /** 有筛选在生效 */
  filtersActive: boolean;
  /** 列表加载状态 */
  listState: RunListState;
  /** 当前页的运行 */
  items: AgentRunDigest[];
  /** 当前页码，从 1 起 */
  page: number;
  /** 每页条数 */
  pageSize: number;
  /** 命中总数 */
  total: number;
  /** 正在换页或换筛选（保留旧数据，半透明） */
  refreshing: boolean;
  /** 抽屉里正在看的运行 */
  activeRid: string | null;
  /** 当前时间（毫秒） */
  now: number;
  onSelectHarness: (key: string) => void;
  onRetryStats: () => void;
  onFilterChange: (patch: Partial<RunFilters>) => void;
  onReset: () => void;
  onOpen: (rid: string) => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
  onRetryList: () => void;
};

/**
 * Agent 运行记录页的画面：标题、版本对比卡片、筛选栏、运行列表（页码分页，底部显示总数和页码）。
 * 详情抽屉是页面状态，由上层单独挂载。
 */
export function AgentRunsView(props: AgentRunsViewProps) {
  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>运行记录</PageHeaderTitle>
        </PageHeaderHeading>
      </PageHeader>

      <VersionCards
        status={props.statsStatus}
        groups={props.harness}
        selectedKey={props.filters.harnessKey}
        now={props.now}
        onSelect={props.onSelectHarness}
        onRetry={props.onRetryStats}
      />

      <DataPanel>
        <RunFilterBar
          filters={props.filters}
          active={props.filtersActive}
          harness={props.harness}
          models={props.models}
          onChange={props.onFilterChange}
          onReset={props.onReset}
        />
        <RunList
          state={props.listState}
          refreshing={props.refreshing}
          items={props.items}
          activeRid={props.activeRid}
          filtersActive={props.filtersActive}
          onOpen={props.onOpen}
          onRetry={props.onRetryList}
          onReset={props.onReset}
        />
        {props.listState === "ready" && props.total > 0 && (
          <DataPanelPagination
            page={props.page}
            pageSize={props.pageSize}
            total={props.total}
            onPageChange={props.onPageChange}
            onPageSizeChange={props.onPageSizeChange}
          />
        )}
      </DataPanel>
    </DataPage>
  );
}
