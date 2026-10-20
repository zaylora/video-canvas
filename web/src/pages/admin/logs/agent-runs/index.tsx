import { SearchX } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";

import { downloadAgentRunTrace, listAgentRuns } from "@/api/admin/agent-runs";
import type { AgentRunItem } from "@/api/admin/agent-runs/type";
import {
  DataPage,
  DataPanel,
  DataPanelBody,
  DataPanelPagination,
} from "@/components/admin-ui/data-panel";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { FilterSelect } from "@/components/admin-ui/filter-select";
import { ListSearch } from "@/components/admin-ui/list-search";
import { LoadFailed } from "@/components/admin-ui/load-failed";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  AGENT_RUN_EMPTY_HINT,
  AGENT_RUN_EMPTY_TITLE,
  AGENT_RUN_FILTER_KEYS,
  AGENT_RUN_RANGES,
  AGENT_RUN_SOURCE_OPTIONS,
  AGENT_RUN_STATUS_OPTIONS,
  DEFAULT_AGENT_RUN_RANGE,
  agentRunFiltersOf,
  agentRunRequest,
  hasAgentRunFilters,
  traceDownloadFailure,
  traceDownloadSuccess,
  traceFileName,
} from "@/utils/admin/agent-runs";
import type { RangeKey } from "@/utils/admin/log-rules";
import { saveBlob } from "@/utils/admin/save-blob";

import { useAdminList } from "../../use-admin-list";
import { RunsTable } from "./runs-table";

/**
 * Agent 运行页（日志管理 › Agent 运行）：列表 + 下载轨迹包，一期不在浏览器里看时间线。
 * 筛选：用户（邮箱或 id）、画布 id、run id、状态、来源、时间范围（默认最近 7 天），都在 URL 里；页码分页。
 * 下载走 zip，成功提示「已记入操作审计」；失败提示可重试，业务错误（轨迹已过期 / 已关闭）直接说明原因并刷新列表。
 * 契约见设计文档 docs/design/Agent调优Harness/Agent调优Harness.md 8.4。
 */
export default function AgentRunsPage() {
  const list = useAdminList<AgentRunItem>(AGENT_RUN_FILTER_KEYS, (query) =>
    listAgentRuns(agentRunRequest(query)),
  );
  const { q, filters: urlFilters, setQ, setFilter, reload } = list;
  const filters = useMemo(() => agentRunFiltersOf({ q, filters: urlFilters }), [q, urlFilters]);

  // 正在打包下载的 run id；允许同时下载多行
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());

  const download = useCallback(
    async (run: AgentRunItem) => {
      setBusy((prev) => new Set(prev).add(run.id));
      try {
        saveBlob(await downloadAgentRunTrace(run.id), traceFileName(run.id));
        const done = traceDownloadSuccess(run);
        if (done.tone === "warning") toast.warning(done.message);
        else toast.success(done.message);
      } catch (error) {
        const failure = traceDownloadFailure(error);
        toast.error(failure.message);
        // 业务错误说明列表上的下载状态已过时（比如刚好过期），刷新后按钮会置灰并写明原因
        if (failure.refresh) reload();
      } finally {
        setBusy((prev) => {
          const next = new Set(prev);
          next.delete(run.id);
          return next;
        });
      }
    },
    [reload],
  );

  const setRange = (range: RangeKey) =>
    setFilter("range", range === DEFAULT_AGENT_RUN_RANGE ? "" : range);

  const loading = list.state === "loading";
  const filtered = hasAgentRunFilters(filters);

  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>Agent 运行</PageHeaderTitle>
          <p className="text-muted-foreground mt-1 text-sm">
            找到出问题的那一轮，下载轨迹包带去分析；每次下载都会记入操作审计，轨迹保留 14 天。
          </p>
        </PageHeaderHeading>
      </PageHeader>

      <DataPanel>
        <div className="flex flex-col gap-2 p-4 sm:flex-row sm:flex-wrap sm:items-center">
          <ListSearch
            value={filters.user}
            onCommit={setQ}
            placeholder="用户邮箱或 id"
            className="sm:w-56"
          />
          <ListSearch
            value={filters.canvasId}
            onCommit={(value) => setFilter("canvas_id", value)}
            placeholder="画布 id"
            className="sm:w-40"
          />
          <ListSearch
            value={filters.runId}
            onCommit={(value) => setFilter("run_id", value)}
            placeholder="run id"
            className="sm:w-36"
          />
          <FilterSelect
            aria-label="状态筛选"
            value={filters.status}
            onValueChange={(status) => setFilter("status", status)}
            options={AGENT_RUN_STATUS_OPTIONS}
          />
          <FilterSelect
            aria-label="来源筛选"
            value={filters.source}
            onValueChange={(source) => setFilter("source", source)}
            options={AGENT_RUN_SOURCE_OPTIONS}
          />
          <OptionSelect
            aria-label="时间范围"
            size="sm"
            className="w-auto text-xs"
            value={filters.range}
            onValueChange={(range) => setRange(range as RangeKey)}
            options={AGENT_RUN_RANGES.map(([value, label]) => ({ value, label }))}
          />
        </div>

        <DataPanelBody>
          {list.state === "error" ? (
            <LoadFailed onRetry={reload} />
          ) : list.state === "ready" && list.items.length === 0 ? (
            <EmptyState className="m-4">
              <EmptyStateIcon>
                <SearchX />
              </EmptyStateIcon>
              <EmptyStateTitle>{AGENT_RUN_EMPTY_TITLE}</EmptyStateTitle>
              <EmptyStateDescription>{AGENT_RUN_EMPTY_HINT}</EmptyStateDescription>
              <EmptyStateActions>
                {filtered ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      // 清除筛选保留时间范围
                      const range = urlFilters.range;
                      list.clear();
                      if (range) setFilter("range", range);
                    }}
                  >
                    清除筛选
                  </Button>
                ) : (
                  filters.range !== "30d" && (
                    <Button variant="outline" size="sm" onClick={() => setRange("30d")}>
                      放宽到近 30 天
                    </Button>
                  )
                )}
              </EmptyStateActions>
            </EmptyState>
          ) : (
            <div className={cn(list.refreshing && !loading && "opacity-60 transition-opacity")}>
              <RunsTable
                items={list.items}
                loading={loading}
                busy={busy}
                onDownload={(run) => void download(run)}
              />
            </div>
          )}
        </DataPanelBody>

        {list.state === "ready" && list.total > 0 && (
          <DataPanelPagination
            page={list.page}
            pageSize={list.pageSize}
            total={list.total}
            onPageChange={list.setPage}
            onPageSizeChange={list.setPageSize}
          />
        )}
      </DataPanel>
    </DataPage>
  );
}
