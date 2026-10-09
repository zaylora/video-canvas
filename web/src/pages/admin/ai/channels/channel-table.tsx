import { useMemo, useState } from "react";
import { Download, KeyRound, Plus, Trash2, X } from "lucide-react";

import type { ChannelView, ConfigListItem, PluginView } from "@/api/admin/ai/type.d";
import { DataTablePagination } from "@/components/admin-ui/data-table-pagination";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { FilterSelect } from "@/components/admin-ui/filter-select";
import { RowAction } from "@/components/admin-ui/row-action";
import { RowIconAction, RowMoreMenu } from "@/components/admin-ui/row-icon-action";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { SearchInput } from "@/components/admin-ui/search-input";
import { StatusLabel } from "@/components/admin-ui/status-dot";
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
import { channelHealth, HEALTH_UI_TONE } from "@/utils/admin/health";
import { availableUpgrade, channelMeta, channelSupportsKind } from "@/utils/admin/plugin";
import { filterChannels, paginate } from "@/utils/admin/table-view";

import { KindFilter, KindIcons, PLUGIN_KIND_ORDER } from "../kind";
import type { LoadStatus } from "../../use-admin";

/** 状态筛选：与状态列一致（可用 / 未设 Key / 不可用 / 已停用），外加“可升级” */
const STATUS_OPTIONS = [
  ["", "全部状态"],
  ["ok", "可用"],
  ["warn", "未设 Key"],
  ["bad", "不可用"],
  ["off", "已停用"],
  ["upgrade", "可升级"],
] as const;

/** 渠道能支持的能力（给“能力”列的小图标用）；读不到插件版本时为空 */
const channelKinds = (plugins: PluginView[], channel: ChannelView) =>
  PLUGIN_KIND_ORDER.filter((kind) => channelSupportsKind(plugins, channel, kind) === true);

/** 渠道页的表格：搜索、筛选、分页，行内图标操作加「⋯」菜单；点整行打开渠道弹窗 */
export function ChannelTable({
  channels,
  plugins,
  status,
  canWrite,
  models,
  modelsStatus,
  onOpen,
  onNew,
  onImport,
  onSetKey,
  onToggle,
  onDelete,
  onRetry,
}: {
  channels: ChannelView[];
  plugins: PluginView[];
  status: LoadStatus;
  canWrite: boolean;
  /** 各渠道当前的生成中 / 排队数，按渠道 key 索引 */
  /** 全部模型：算每个渠道有几个模型 */
  models: ConfigListItem[];
  modelsStatus: LoadStatus;
  onOpen: (key: string) => void;
  onNew: () => void;
  onImport: (channel: ChannelView) => void;
  onSetKey: (channel: ChannelView) => void;
  onToggle: (channel: ChannelView) => void;
  onDelete: (channel: ChannelView) => void;
  onRetry: () => void;
}) {
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState("");
  const [plugin, setPlugin] = useState("");
  const [state, setState] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  /** 改筛选条件后回到第一页 */
  const filter = (setter: (value: string) => void) => (value: string) => {
    setter(value);
    setPage(1);
  };
  const filtering = !!(query || kind || plugin || state);
  const reset = () => {
    setQuery("");
    setKind("");
    setPlugin("");
    setState("");
    setPage(1);
  };

  const shown = useMemo(
    () => filterChannels(channels, plugins, { query, kind, plugin, status: state }),
    [channels, plugins, query, kind, plugin, state],
  );
  const { rows, page: current } = paginate(shown, page, pageSize);
  const modelCount = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const item of models)
      if (item.channel) counts[item.channel] = (counts[item.channel] ?? 0) + 1;
    return counts;
  }, [models]);
  const columns = 7;

  if (status === "ready" && channels.length === 0) {
    return (
      <EmptyState>
        <EmptyStateTitle>还没有渠道</EmptyStateTitle>
        <EmptyStateDescription>
          渠道把一个插件版本、一个地址和一个 Key 绑在一起，模型通过渠道调用上游。
          {!canWrite && "请联系运维新建。"}
        </EmptyStateDescription>
        {canWrite && (
          <EmptyStateActions>
            <Button onClick={onNew}>
              <Plus />
              新建渠道
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
          placeholder="搜索名称、key 或地址"
          aria-label="搜索渠道"
          className="w-full sm:w-64 [&_input]:h-8"
        />
        <KindFilter value={kind} onChange={filter(setKind)} slideId="channel-kind" />
        <FilterSelect
          aria-label="按插件筛选"
          value={plugin}
          onChange={(event) => filter(setPlugin)(event.target.value)}
          options={[["", "全部插件"], ...plugins.map((item) => [item.key, item.name] as const)]}
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
        <TableToolbarCount shown={shown.length} total={channels.length} />
      </TableToolbar>

      <div className="flex flex-col gap-4 px-4 pb-4">
        <div className="overflow-hidden rounded-md border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground px-3 text-xs">渠道</TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs">插件</TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs max-md:hidden">
                  能力
                </TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs">模型</TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs">状态</TableHead>
                <TableHead className="text-muted-foreground px-3 text-xs">启用</TableHead>
                <TableHead className="text-muted-foreground px-3 text-right text-xs">
                  操作
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {status === "loading" &&
                Array.from({ length: 4 }, (_, index) => (
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
                    没有匹配的渠道
                  </TableCell>
                </TableRow>
              )}
              {status === "ready" &&
                rows.map((channel) => {
                  const owner = plugins.find((item) => item.key === channel.plugin_key);
                  const health = channelHealth(channel, plugins);
                  const upgrade = availableUpgrade(plugins, channel);
                  const meta = channelMeta(plugins, channel);
                  const importUnsupported = !!meta && !meta.import;
                  // 状态列第二行：只在“不可用”时写原因，其余看标签就够了
                  const note =
                    health.tone === "bad" && health.reason ? (
                      <div className="text-muted-foreground mt-1 max-w-52 truncate text-xs">
                        {health.reason}
                      </div>
                    ) : null;
                  return (
                    <TableRow
                      key={channel.key}
                      data-channel={channel.key}
                      className="cursor-pointer"
                      onClick={() => onOpen(channel.key)}
                    >
                      <TableCell className="px-3 py-3">
                        <button
                          type="button"
                          className="focus-visible:ring-ring/50 max-w-56 rounded text-left font-medium outline-none focus-visible:ring-[3px]"
                        >
                          <span className="block truncate">{channel.name}</span>
                        </button>
                        <div className="text-muted-foreground max-w-56 truncate font-mono text-xs">
                          {channel.key}
                        </div>
                      </TableCell>
                      <TableCell className="px-3 py-3">
                        <div>
                          {owner?.name ?? channel.plugin_key}{" "}
                          <span className="text-muted-foreground font-mono text-xs">
                            v{channel.plugin_version}
                          </span>
                        </div>
                        {upgrade && (
                          <div className="text-muted-foreground text-xs">
                            可升级到 v{upgrade.version}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className="px-3 py-3 max-md:hidden">
                        <KindIcons kinds={channelKinds(plugins, channel)} />
                      </TableCell>
                      <TableCell className="px-3 py-3 tabular-nums">
                        {modelsStatus === "ready" ? (modelCount[channel.key] ?? 0) : "—"}
                      </TableCell>
                      <TableCell className="px-3 py-3">
                        <StatusLabel tone={HEALTH_UI_TONE[health.tone]}>{health.label}</StatusLabel>
                        {note}
                      </TableCell>
                      <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                        <Switch
                          checked={channel.enabled}
                          disabled={!canWrite}
                          title={canWrite ? undefined : "需要运维权限"}
                          aria-label={`${channel.enabled ? "停用" : "启用"} ${channel.name}`}
                          onCheckedChange={() => onToggle(channel)}
                        />
                      </TableCell>
                      <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                        <div className="flex items-center justify-end gap-0.5">
                          <RowIconAction
                            label="导入模型"
                            target={channel.name}
                            deny={importUnsupported ? "该插件不支持导入模型" : null}
                            onClick={() => onImport(channel)}
                          >
                            <Download />
                          </RowIconAction>
                          {canWrite && (
                            <>
                              <RowIconAction
                                label={channel.secret_set ? "更新 Key" : "设置 Key"}
                                target={channel.name}
                                onClick={() => onSetKey(channel)}
                              >
                                <KeyRound />
                              </RowIconAction>
                              <RowMoreMenu label={`${channel.name} 的更多操作`}>
                                <DropdownMenuItem
                                  variant="destructive"
                                  onClick={() => onDelete(channel)}
                                >
                                  <Trash2 />
                                  删除
                                </DropdownMenuItem>
                              </RowMoreMenu>
                            </>
                          )}
                        </div>
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
