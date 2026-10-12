import { CircleArrowUp, Trash2, Upload, X } from "lucide-react";

import type { PluginListItem } from "@/api/admin/ai/type.d";
import {
  DataPanel,
  DataPanelBody,
  DataPanelPagination,
} from "@/components/admin-ui/data-panel";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { FilterSelect } from "@/components/admin-ui/filter-select";
import { RowAction } from "@/components/admin-ui/row-action";
import {
  RowIconAction,
  RowMoreMenu,
} from "@/components/admin-ui/row-icon-action";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { ListSearch } from "@/components/admin-ui/list-search";
import { Tag } from "@/components/admin-ui/tag";
import {
  TableToolbar,
  TableToolbarCount,
} from "@/components/admin-ui/table-toolbar";
import { Button } from "@/components/ui/button";
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
import { KindFilter, KindIcons } from "../kind";
import type { useAdminList } from "../../use-admin-list";

/** 状态筛选：启用 / 停用，外加“有旧版渠道”（有渠道固定在旧版本上，可以升级） */
const STATUS_OPTIONS = [
  ["", "全部状态"],
  ["on", "已启用"],
  ["off", "已停用"],
  ["outdated", "有旧版渠道"],
] as const;

const SOURCE_OPTIONS = [
  ["", "全部来源"],
  ["builtin", "内置"],
  ["uploaded", "已上传"],
] as const;

/** 插件页的表格：搜索、筛选、分页都在服务端，行内图标操作加「⋯」菜单；点整行打开插件弹窗 */
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
  const columns = 7;

  if (status === "ready" && list.total === 0 && !list.filtering) {
    return (
      <EmptyState>
        <EmptyStateTitle>还没有插件</EmptyStateTitle>
        {canWrite && (
          <EmptyStateActions>
            <Button onClick={onUpload}>
              <Upload />
              上传插件
            </Button>
          </EmptyStateActions>
        )}
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
              <TableHead className="text-muted-foreground px-3 text-xs">
                插件
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs max-md:hidden">
                来源
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs max-md:hidden">
                能力
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">
                最新版本
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">
                渠道
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">
                启用
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-right text-xs">
                操作
              </TableHead>
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
                <TableCell
                  colSpan={columns}
                  className="text-destructive h-32 text-center"
                >
                  加载失败
                  <button
                    type="button"
                    className="ml-1 underline"
                    onClick={list.reload}
                  >
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
                const latest = plugin.versions.find(
                  (v) => v.version === plugin.latest_version,
                );
                const outdated = plugin.outdated_channel_count;
                return (
                  <TableRow
                    key={plugin.key}
                    data-plugin={plugin.key}
                    className="cursor-pointer"
                    onClick={() => onOpen(plugin.key)}
                  >
                    <TableCell className="px-3 py-3">
                      <button
                        type="button"
                        className="focus-visible:ring-ring/50 max-w-56 rounded text-left font-medium outline-none focus-visible:ring-[3px]"
                      >
                        <span className="block truncate">{plugin.name}</span>
                      </button>
                      <div className="text-muted-foreground max-w-56 truncate font-mono text-xs">
                        {plugin.key}
                      </div>
                    </TableCell>
                    <TableCell className="px-3 py-3 max-md:hidden">
                      <Tag>
                        {plugin.source === "builtin" ? "内置" : "已上传"}
                      </Tag>
                    </TableCell>
                    <TableCell className="px-3 py-3 max-md:hidden">
                      <KindIcons
                        kinds={Object.keys(latest?.meta?.endpoints ?? {})}
                      />
                    </TableCell>
                    <TableCell className="px-3 py-3 font-mono text-xs">
                      {plugin.latest_version
                        ? `v${plugin.latest_version}`
                        : "—"}
                    </TableCell>
                    <TableCell className="px-3 py-3 text-xs whitespace-nowrap tabular-nums">
                      {plugin.channel_count} 个在用
                      {outdated > 0 && (
                        <span className="text-muted-foreground">
                          {" "}
                          · {outdated} 个旧版
                        </span>
                      )}
                    </TableCell>
                    <TableCell
                      className="px-3 py-3"
                      onClick={(event) => event.stopPropagation()}
                    >
                      <Switch
                        checked={plugin.enabled}
                        disabled={!canWrite || toggling === plugin.key}
                        title={canWrite ? undefined : "需要运维权限"}
                        aria-label={`${plugin.enabled ? "停用" : "启用"}插件 ${plugin.name}`}
                        onCheckedChange={(checked) => onToggle(plugin, checked)}
                      />
                    </TableCell>
                    <TableCell
                      className="px-3 py-3"
                      onClick={(event) => event.stopPropagation()}
                    >
                      {canWrite && (
                        <div className="flex items-center justify-end gap-0.5">
                          <RowIconAction
                            label={
                              outdated > 0
                                ? `升级渠道（${outdated}）`
                                : "升级渠道"
                            }
                            target={plugin.name}
                            deny={outdated === 0 ? "渠道都已是最新版本" : null}
                            onClick={() => onUpgrade(plugin)}
                          >
                            <CircleArrowUp />
                          </RowIconAction>
                          <RowMoreMenu label={`${plugin.name} 的更多操作`}>
                            <DropdownMenuItem
                              variant="destructive"
                              onClick={() => onDelete(plugin)}
                            >
                              <Trash2 />
                              删除插件
                            </DropdownMenuItem>
                          </RowMoreMenu>
                        </div>
                      )}
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
