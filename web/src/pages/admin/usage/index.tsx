import { SearchX } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router";

import { getUsageSummary, listUsage } from "@/api/admin/usage";
import type { UsageItem, UsageSummary } from "@/api/admin/usage/type";
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
import { LoadFailed } from "@/components/admin-ui/load-failed";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  hasUsageFilters,
  isTaskRef,
  readUserFilterState,
  USAGE_FILTER_KEYS,
  usageFilterEntry,
  usageFiltersOf,
  usageRequest,
  usageSummaryRequest,
  USAGE_RANGES,
  type UsageFilters,
} from "@/utils/admin/log-rules";

import { withListParam } from "@/utils/admin/list-params";

import { useAdminList } from "../use-admin-list";
import { TaskDialog } from "./task-dialog";
import { UsageFilterBar } from "./usage-filters";
import { UsageOverview } from "./usage-summary";
import { UsageTable } from "./usage-table";

/**
 * 使用记录页（用户 › 使用记录）：概览卡 + 筛选条 + 任务表格 + 页码分页，点一行从右侧抽屉看详情。
 * 搜索词、筛选、页码都在 URL 里（刷新不丢）；翻页和筛选时保留旧数据并半透明，改筛选回到第 1 页。
 * 详情抽屉只是页面状态，不写进 URL；从用户管理进入时通过路由 state 带 `{ userId }`，读完写进 URL 并清空 state。
 * 请求失败的全局 toast 由拦截器弹，这里只给「加载失败 · 重试」。
 */
export default function UsagePage() {
  const location = useLocation();
  const navigate = useNavigate();
  const list = useAdminList<UsageItem>(USAGE_FILTER_KEYS, (query) =>
    listUsage(usageRequest(query)),
  );
  const { q, filters: urlFilters, setFilter } = list;
  const filters = useMemo<UsageFilters>(
    () => usageFiltersOf({ q, filters: urlFilters }),
    [q, urlFilters],
  );

  // 用户管理的「查看全部使用记录」：把 state 里的 userId 写进 URL 的 user_id，同时清空 state，刷新后不再重复
  useEffect(() => {
    const userId = readUserFilterState(location.state);
    if (userId === null) return;
    navigate(
      {
        pathname: location.pathname,
        search: withListParam(
          new URLSearchParams(location.search),
          "user_id",
          String(userId),
        ).toString(),
      },
      { replace: true, state: null },
    );
  }, [location.state, location.pathname, location.search, navigate]);

  const patchFilters = useCallback(
    (patch: Partial<UsageFilters>) => {
      const [key, value] = usageFilterEntry(patch);
      setFilter(key, value);
    },
    [setFilter],
  );

  // 摘要与列表用同一组筛选；翻页不重新取
  const summaryKey = JSON.stringify({ q, urlFilters });
  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [summaryLoading, setSummaryLoading] = useState(true);
  useEffect(() => {
    let alive = true;
    setSummaryLoading(true);
    const query = JSON.parse(summaryKey) as { q: string; urlFilters: Record<string, string> };
    getUsageSummary(usageSummaryRequest({ q: query.q, filters: query.urlFilters }))
      .then((next) => {
        if (alive) setSummary(next);
      })
      .catch(() => {
        if (alive) setSummary(null);
      })
      .finally(() => {
        if (alive) setSummaryLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [summaryKey]);

  // 抽屉：关闭时保留任务编号，等关闭动画播完
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerRef, setDrawerRef] = useState<string | null>(null);
  const openTask = useCallback((ref: string) => {
    setDrawerRef(ref);
    setDrawerOpen(true);
  }, []);
  const filterUser = useCallback(
    (user: { id: number; username: string }) => {
      patchFilters({ userId: user.id });
      setDrawerOpen(false);
    },
    [patchFilters],
  );

  const userName = useMemo(
    () => list.items.find((item) => item.user.id === filters.userId)?.user.username,
    [list.items, filters.userId],
  );

  // 清除筛选保留时间范围
  const clearFilters = () => {
    const range = urlFilters.range;
    list.clear();
    if (range) setFilter("range", range);
  };
  const widenRange = () => patchFilters({ range: "30d" });

  const loading = list.state === "loading";
  const filtered = hasUsageFilters(filters);
  const lookingForRef = isTaskRef(filters.q);

  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>使用记录</PageHeaderTitle>
          <p className="text-muted-foreground mt-1 text-sm">
            查看谁生成了什么、结果怎么样；点一条看详情。
          </p>
        </PageHeaderHeading>
      </PageHeader>

      <DataPanel>
        <div className="flex flex-col gap-4 p-4">
          {list.state !== "error" && (
            <UsageOverview
              summary={summary}
              loading={summaryLoading}
              status={filters.status}
              rangeLabel={USAGE_RANGES.find(([value]) => value === filters.range)?.[1] ?? ""}
              onStatus={(status) => patchFilters({ status })}
            />
          )}
          <UsageFilterBar filters={filters} userName={userName} onChange={patchFilters} />
        </div>

        <DataPanelBody>
          {list.state === "error" ? (
            <LoadFailed onRetry={list.reload} />
          ) : list.state === "ready" && list.items.length === 0 ? (
            <EmptyState className="m-4">
              <EmptyStateIcon>
                <SearchX />
              </EmptyStateIcon>
              <EmptyStateTitle>
                {lookingForRef
                  ? "找不到这个任务编号"
                  : filtered
                    ? "没有符合条件的记录"
                    : "这个时间范围内还没有人生成过内容"}
              </EmptyStateTitle>
              <EmptyStateDescription>
                {filtered ? "换个条件试试，或清除筛选。" : "可以放宽到近 30 天再看看。"}
              </EmptyStateDescription>
              <EmptyStateActions>
                {filtered ? (
                  <Button variant="outline" size="sm" onClick={clearFilters}>
                    清除筛选
                  </Button>
                ) : (
                  filters.range !== "30d" && (
                    <Button variant="outline" size="sm" onClick={widenRange}>
                      放宽到近 30 天
                    </Button>
                  )
                )}
              </EmptyStateActions>
            </EmptyState>
          ) : (
            <div className={cn(list.refreshing && !loading && "opacity-60 transition-opacity")}>
              <UsageTable
                items={list.items}
                loading={loading}
                activeRef={drawerOpen ? drawerRef : null}
                onOpen={openTask}
                onFilterUser={filterUser}
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

      <TaskDialog
        open={drawerOpen}
        taskRef={drawerRef}
        onClose={() => setDrawerOpen(false)}
        onFilterUser={filterUser}
      />
    </DataPage>
  );
}
