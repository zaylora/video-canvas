import { CircleArrowUp, Trash2, Upload, X } from "lucide-react";

import type { PluginListItem } from "@/api/admin/ai/type.d";
import { DataPanel, DataPanelBody, DataPanelPagination } from "@/components/admin-ui/data-panel";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { FilterSelect } from "@/components/admin-ui/filter-select";
import { ListSearch } from "@/components/admin-ui/list-search";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { RowAction } from "@/components/admin-ui/row-action";
import { RowMoreMenu } from "@/components/admin-ui/row-icon-action";
import { Tag } from "@/components/admin-ui/tag";
import { TableToolbar, TableToolbarCount } from "@/components/admin-ui/table-toolbar";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { vendorOf } from "@/utils/admin/plugin";

import { KindFilter, KindTag } from "../kind";
import type { useAdminList } from "../../use-admin-list";

/** admin 看到的写操作置灰时的原因（置灰而不是隐藏，让运营知道这里有这个能力） */
export const SUPER_ONLY = "仅 super_admin 可用";

/** 状态筛选：启用 / 停用，外加“有旧版模型”（有模型的协议固定在旧版本上，可以升级） */
const STATUS_OPTIONS = [
  ["", "全部状态"],
  ["on", "已启用"],
  ["off", "已停用"],
  ["outdated", "有旧版模型"],
] as const;

const SOURCE_OPTIONS = [
  ["", "全部来源"],
  ["builtin", "内置"],
  ["uploaded", "已上传"],
] as const;

/**
 * 插件页的表格：搜索、筛选、分页都在服务端；点整行打开插件弹窗。
 * 列：插件（名称 + 厂商 · key）、能力（同步 / 异步）、最新版本、模型（在用 / 旧版）、启用、操作（升级模型 + 更多）。
 * 写操作（启停、升级模型、上传、删除）只有 super_admin 能做：admin 看到的是置灰的按钮并写明原因，不隐藏。
 */
export function PluginTable({
  list,
  canWrite,
  toggling,
  onOpen,
  onUpload,
  onToggle,
  onUpgrade,
  onDelete,
}: {
  /** 插件分页列表（useAdminList）：当页的行、总数、搜索 / 筛选 / 翻页 */
  list: ReturnType<typeof useAdminList<PluginListItem>>;
  /** 是否是 super_admin；false 时写操作置灰 */
  canWrite: boolean;
  /** 正在启停的插件 key */
  toggling: string | null;
  onOpen: (key: string) => void;
  onUpload: () => void;
  onToggle: (plugin: PluginListItem, enabled: boolean) => void;
  onUpgrade: (plugin: PluginListItem) => void;
  onDelete: (plugin: PluginListItem) => void;
}) {
  const { items: rows, state: status } = list;
  const columns = 6;

  if (status === "ready" && list.total === 0 && !list.filtering) {
    return (
      <EmptyState>
        <EmptyStateTitle>还没有插件</EmptyStateTitle>
        <EmptyStateActions>
          <ReasonTooltip reason={canWrite ? null : SUPER_ONLY}>
            <Button disabled={!canWrite} onClick={onUpload}>
              <Upload />
              上传插件
            </Button>
          </ReasonTooltip>
        </EmptyStateActions>
      </EmptyState>
    );
  }

  return (
    <DataPanel>
      <TableToolbar>
        <ListSearch
          value={list.q}
          onCommit={list.setQ}
          placeholder="搜索名称或 key"
          className="[&_input]:h-8"
        />
        <KindFilter
          value={list.filters.kind ?? ""}
          onChange={(value) => list.setFilter("kind", value)}
          slideId="plugin-kind"
        />
        <FilterSelect
          aria-label="按来源筛选"
          value={list.filters.source ?? ""}
          onValueChange={(value) => list.setFilter("source", value)}
          options={SOURCE_OPTIONS}
        />
        <FilterSelect
          aria-label="按状态筛选"
          value={list.filters.status ?? ""}
          onValueChange={(value) => list.setFilter("status", value)}
          options={STATUS_OPTIONS}
        />
        {list.filtering && (
          <RowAction onClick={list.clear}>
            <X />
            重置
          </RowAction>
        )}
        <TableToolbarCount shown={rows.length} total={list.total} />
      </TableToolbar>

      <DataPanelBody>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="text-muted-foreground px-3 text-xs">插件</TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs max-md:hidden">
                能力
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">最新版本</TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">模型</TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">启用</TableHead>
              <TableHead className="text-muted-foreground px-3 text-right text-xs">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {status === "loading" &&
              Array.from({ length: 3 }, (_, index) => (
                <TableRow key={index}>
                  <TableCell colSpan={columns} className="px-3 py-3">
                    <Skeleton className="h-9" />
                  </TableCell>
                </TableRow>
              ))}
            {status === "error" && (
              <TableRow>
                <TableCell colSpan={columns} className="text-destructive h-32 text-center">
                  加载失败
                  <button type="button" className="ml-1 underline" onClick={list.reload}>
                    重试
                  </button>
                </TableCell>
              </TableRow>
            )}
            {status === "ready" && rows.length === 0 && (
              <TableRow className="hover:bg-transparent">
                <TableCell
                  colSpan={columns}
                  className="text-muted-foreground h-32 text-center text-sm"
                >
                  没有匹配的插件
                </TableCell>
              </TableRow>
            )}
            {status === "ready" &&
              rows.map((plugin) => {
                const latest = plugin.versions.find((v) => v.version === plugin.latest_version);
                const endpoints = Object.entries(latest?.meta?.endpoints ?? {});
                const vendor = vendorOf(latest?.meta);
                const outdated = plugin.outdated_model_count;
                const upgradeDeny = !canWrite
                  ? SUPER_ONLY
                  : outdated === 0
                    ? "没有固定在旧版本上的模型"
                    : null;
                return (
                  <TableRow
                    key={plugin.key}
                    data-plugin={plugin.key}
                    className="cursor-pointer"
                    onClick={() => onOpen(plugin.key)}
                  >
                    <TableCell className="px-3 py-3">
                      <div className="flex max-w-64 items-center gap-1.5">
                        <button
                          type="button"
                          className="focus-visible:ring-ring/50 min-w-0 rounded text-left font-medium outline-none focus-visible:ring-[3px]"
                        >
                          <span className="block truncate">{plugin.name}</span>
                        </button>
                        {plugin.source === "builtin" && <Tag className="shrink-0">内置</Tag>}
                      </div>
                      <div className="text-muted-foreground max-w-64 truncate text-xs">
                        {vendor && `${vendor} · `}
                        <span className="font-mono">{plugin.key}</span>
                      </div>
                    </TableCell>
                    <TableCell className="px-3 py-3 max-md:hidden">
                      {endpoints.length === 0 ? (
                        <span className="text-muted-foreground text-xs">—</span>
                      ) : (
                        <div className="flex flex-wrap gap-x-3 gap-y-1">
                          {endpoints.map(([kind, endpoint]) => (
                            <span key={kind} className="inline-flex items-center gap-1">
                              <KindTag kind={kind} />
                              <span className="text-muted-foreground text-[11px]">
                                {endpoint?.mode === "async" ? "异步" : "同步"}
                              </span>
                            </span>
                          ))}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="px-3 py-3 font-mono text-xs">
                      {plugin.latest_version ? `v${plugin.latest_version}` : "—"}
                    </TableCell>
                    <TableCell className="px-3 py-3 text-xs whitespace-nowrap tabular-nums">
                      {plugin.model_count > 0 ? (
                        <>
                          {plugin.model_count} 个在用
                          {outdated > 0 && (
                            <>
                              {" · "}
                              <Tag tone="warning">{outdated} 个旧版</Tag>
                            </>
                          )}
                        </>
                      ) : (
                        <span className="text-muted-foreground">未使用</span>
                      )}
                    </TableCell>
                    <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                      <ReasonTooltip reason={canWrite ? null : SUPER_ONLY}>
                        <Switch
                          checked={plugin.enabled}
                          disabled={!canWrite || toggling === plugin.key}
                          aria-label={`${plugin.enabled ? "停用" : "启用"}插件 ${plugin.name}`}
                          onCheckedChange={(checked) => onToggle(plugin, checked)}
                        />
                      </ReasonTooltip>
                    </TableCell>
                    <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                      <div className="flex items-center justify-end gap-0.5">
                        <ReasonTooltip reason={upgradeDeny}>
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={!!upgradeDeny}
                            aria-label={`升级模型 ${plugin.name}`}
                            onClick={() => onUpgrade(plugin)}
                          >
                            <CircleArrowUp />
                            升级模型{outdated > 0 && `（${outdated}）`}
                          </Button>
                        </ReasonTooltip>
                        <RowMoreMenu label={`${plugin.name} 的更多操作`}>
                          <DropdownMenuItem
                            variant="destructive"
                            disabled={!canWrite}
                            onClick={() => onDelete(plugin)}
                          >
                            <Trash2 />
                            删除插件{!canWrite && `（${SUPER_ONLY}）`}
                          </DropdownMenuItem>
                        </RowMoreMenu>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
          </TableBody>
        </Table>
      </DataPanelBody>
      {status === "ready" && (
        <DataPanelPagination
          page={list.page}
          pageSize={list.pageSize}
          total={list.total}
          onPageChange={list.setPage}
          onPageSizeChange={list.setPageSize}
        />
      )}
    </DataPanel>
  );
}
