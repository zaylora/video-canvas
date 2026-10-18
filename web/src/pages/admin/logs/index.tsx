import { SearchX } from "lucide-react";
import { useCallback, useMemo, useState } from "react";

import { listAudits } from "@/api/admin/logs";
import type { AuditItem, AuditModule } from "@/api/admin/logs/type";
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
  AUDIT_MODULES,
  AUDIT_RANGES,
  AUDIT_FILTER_KEYS,
  auditFiltersOf,
  auditRequest,
  type AuditFilters,
  type RangeKey,
} from "@/utils/admin/log-rules";

import { useAdminList } from "../use-admin-list";
import { AuditDrawer } from "./audit-drawer";
import { AuditTable } from "./audit-table";

/**
 * 操作审计页（日志管理 › 操作审计）：admin 与 ai 两张审计表合并后的只读卡片列表，按时间倒序，页码分页。
 * 筛选：操作人、模块、时间范围（默认近 30 天）；操作人、模块、范围、页码都在 URL 里。点行从右侧抽屉看详情，抽屉不写进 URL。
 */
export default function AuditsPage() {
  const list = useAdminList<AuditItem>(AUDIT_FILTER_KEYS, (query) =>
    listAudits(auditRequest(query)),
  );
  const { q, filters: urlFilters, setQ, setFilter } = list;
  const filters = useMemo<AuditFilters>(
    () => auditFiltersOf({ q, filters: urlFilters }),
    [q, urlFilters],
  );
  const setActor = setQ;
  const patch = useCallback(
    (next: Partial<AuditFilters>) => {
      if ("module" in next) setFilter("module", next.module ?? "");
      if ("range" in next) setFilter("range", next.range === "30d" ? "" : (next.range ?? ""));
    },
    [setFilter],
  );

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selected, setSelected] = useState<AuditItem | null>(null);

  const loading = list.state === "loading";
  const filtered = filters.actor.trim() !== "" || filters.module !== "";

  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>操作审计</PageHeaderTitle>
          <p className="text-muted-foreground mt-1 text-sm">看看最近后台有谁做了什么调整。</p>
        </PageHeaderHeading>
      </PageHeader>

      <DataPanel>
        <div className="flex flex-col gap-2 p-4 sm:flex-row sm:flex-wrap sm:items-center">
          <ListSearch
            value={filters.actor}
            onCommit={setActor}
            placeholder="输入操作人的用户名"
            className="sm:w-64"
          />
          <FilterSelect
            aria-label="分类筛选"
            value={filters.module}
            onValueChange={(module) => patch({ module: module as AuditModule | "" })}
            options={AUDIT_MODULES.map(([value, label]) => [
              value,
              value === "" ? "全部分类" : label,
            ])}
          />
          <OptionSelect
            aria-label="时间范围"
            size="sm"
            className="w-auto text-xs"
            value={filters.range}
            onValueChange={(range) => patch({ range: range as RangeKey })}
            options={AUDIT_RANGES.map(([value, label]) => ({ value, label }))}
          />
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
                {filtered ? "没有符合条件的记录" : "这个时间范围内没有操作记录"}
              </EmptyStateTitle>
              <EmptyStateDescription>
                {filtered ? "换个条件试试，或清除筛选。" : "可以放宽时间范围再看看。"}
              </EmptyStateDescription>
              <EmptyStateActions>
                {filtered ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      const range = urlFilters.range;
                      list.clear();
                      if (range) setFilter("range", range);
                    }}
                  >
                    清除筛选
                  </Button>
                ) : (
                  filters.range !== "90d" && (
                    <Button variant="outline" size="sm" onClick={() => patch({ range: "90d" })}>
                      放宽时间范围
                    </Button>
                  )
                )}
              </EmptyStateActions>
            </EmptyState>
          ) : (
            <div className={cn(list.refreshing && !loading && "opacity-60 transition-opacity")}>
              <AuditTable
                items={list.items}
                loading={loading}
                onOpen={(item) => {
                  setSelected(item);
                  setDrawerOpen(true);
                }}
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

      <AuditDrawer open={drawerOpen} item={selected} onOpenChange={setDrawerOpen} />
    </DataPage>
  );
}
