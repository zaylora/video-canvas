import { useMemo, useState } from "react";
import { CircleArrowUp, Trash2, Upload, X } from "lucide-react";

import type { ChannelView, PluginView } from "@/api/admin/ai/type.d";
import { DataTablePagination } from "@/components/admin-ui/data-table-pagination";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { FilterSelect } from "@/components/admin-ui/filter-select";
import { RowAction } from "@/components/admin-ui/row-action";
import { RowIconAction, RowMoreMenu } from "@/components/admin-ui/row-icon-action";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { SearchInput } from "@/components/admin-ui/search-input";
import { Tag } from "@/components/admin-ui/tag";
import { TableToolbar, TableToolbarCount } from "@/components/admin-ui/table-toolbar";
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
import { latestVersion, pluginChannelCount } from "@/utils/admin/plugin";
import { filterPlugins, paginate } from "@/utils/admin/table-view";

import { KindFilter, KindIcons } from "../kind";
import type { LoadStatus } from "../../use-admin";
import { outdatedChannels } from "./upgrade-channels";

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

/** 插件页的表格：搜索、筛选、分页，行内图标操作加「⋯」菜单；点整行打开插件弹窗 */
export function PluginTable({
  plugins,
  channels,
  status,
  canWrite,
  toggling,
  onOpen,
  onUpload,
  onToggle,
  onUpgrade,
  onDelete,
  onRetry,
}: {
  plugins: PluginView[];
  channels: ChannelView[];
  status: LoadStatus;
  canWrite: boolean;
  /** 正在启停的插件 key */
  toggling: string | null;
  onOpen: (key: string) => void;
  onUpload: () => void;
  onToggle: (plugin: PluginView, enabled: boolean) => void;
  onUpgrade: (plugin: PluginView) => void;
  onDelete: (plugin: PluginView) => void;
  onRetry: () => void;
}) {
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState("");
  const [source, setSource] = useState("");
  const [state, setState] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  /** 改筛选条件后回到第一页 */
  const filter = (setter: (value: string) => void) => (value: string) => {
    setter(value);
    setPage(1);
  };
  const filtering = !!(query || kind || source || state);
  const reset = () => {
    setQuery("");
    setKind("");
    setSource("");
    setState("");
    setPage(1);
  };

  const shown = useMemo(
    () => filterPlugins(plugins, channels, { query, kind, source, status: state }),
    [plugins, channels, query, kind, source, state],
  );
  const { rows, page: current } = paginate(shown, page, pageSize);
  const columns = 7;

  if (status === "ready" && plugins.length === 0) {
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
    <section className="bg-card rounded-xl border">
      <TableToolbar>
        <SearchInput
          value={query}
          onChange={(event) => filter(setQuery)(event.target.value)}
          placeholder="搜索名称或 key"
          aria-label="搜索插件"
          className="w-full sm:w-64 [&_input]:h-8"
        />
        <KindFilter value={kind} onChange={filter(setKind)} slideId="plugin-kind" />
        <FilterSelect
          aria-label="按来源筛选"
          value={source}
          onChange={(event) => filter(setSource)(event.target.value)}
          options={SOURCE_OPTIONS}
        />
        <FilterSelect
          aria-label="按状态筛选"
          value={state}
          onChange={(event) => filter(setState)(event.target.value)}
          options={STATUS_OPTIONS}
        />
        {filtering && (
          <RowAction onClick={reset}>
            <X />
            重置
          </RowAction>
        )}
        <TableToolbarCount shown={shown.length} total={plugins.length} />
      </TableToolbar>

      <div className="flex flex-col gap-4 px-4 pb-4">
        <div className="overflow-hidden rounded-md border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground px-3 text-xs">插件</TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs max-md:hidden">
                  来源
                </TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs max-md:hidden">
                  能力
                </TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs">最新版本</TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs">渠道</TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs">启用</TableHead>
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
                  <TableCell colSpan={columns} className="text-destructive h-32 text-center">
                    加载失败
                    <button type="button" className="ml-1 underline" onClick={onRetry}>
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
                  const latest = latestVersion(plugin);
                  const outdated = outdatedChannels(plugin, channels).length;
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
                        <Tag>{plugin.source === "builtin" ? "内置" : "已上传"}</Tag>
                      </TableCell>
                      <TableCell className="px-3 py-3 max-md:hidden">
                        <KindIcons kinds={Object.keys(latest?.meta?.endpoints ?? {})} />
                      </TableCell>
                      <TableCell className="px-3 py-3 font-mono text-xs">
                        {latest ? `v${latest.version}` : "—"}
                      </TableCell>
                      <TableCell className="px-3 py-3 text-xs whitespace-nowrap tabular-nums">
                        {pluginChannelCount(plugin)} 个在用
                        {outdated > 0 && (
                          <span className="text-muted-foreground"> · {outdated} 个旧版</span>
                        )}
                      </TableCell>
                      <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                        <Switch
                          checked={plugin.enabled}
                          disabled={!canWrite || toggling === plugin.key}
                          title={canWrite ? undefined : "需要运维权限"}
                          aria-label={`${plugin.enabled ? "停用" : "启用"}插件 ${plugin.name}`}
                          onCheckedChange={(checked) => onToggle(plugin, checked)}
                        />
                      </TableCell>
                      <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                        {canWrite && (
                          <div className="flex items-center justify-end gap-0.5">
                            <RowIconAction
                              label={outdated > 0 ? `升级渠道（${outdated}）` : "升级渠道"}
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
        </div>
        {status === "ready" && (
          <DataTablePagination
            page={current}
            pageSize={pageSize}
            total={shown.length}
            onPageChange={setPage}
            onPageSizeChange={(size) => {
              setPageSize(size);
              setPage(1);
            }}
          />
        )}
      </div>
    </section>
  );
}
