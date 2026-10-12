import { Download, KeyRound, Plus, Trash2, X } from "lucide-react";

import type { ChannelListItem, PluginView } from "@/api/admin/ai/type.d";
import { DataPanel, DataPanelBody, DataPanelPagination } from "@/components/admin-ui/data-panel";
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
import { ListSearch } from "@/components/admin-ui/list-search";
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
import { HEALTH_UI_TONE } from "@/utils/admin/health";

import { KindFilter, KindIcons, PLUGIN_KIND_ORDER } from "../kind";
import type { useAdminList } from "../../use-admin-list";

/** 状态筛选：与状态列一致（可用 / 未设 Key / 不可用 / 已停用），外加“可升级” */
const STATUS_OPTIONS = [
  ["", "全部状态"],
  ["ok", "可用"],
  ["warn", "未设 Key"],
  ["bad", "不可用"],
  ["off", "已停用"],
  ["upgrade", "可升级"],
] as const;

/** 渠道所用插件版本是否支持“导入模型”；读不到插件版本的 meta 时不当成“不支持” */
const importable = (owner: PluginView, channel: ChannelListItem) => {
  const meta = owner.versions.find((v) => v.id === channel.plugin_version_id)?.meta;
  return !meta || !!meta.import;
};

/** 渠道能支持的能力（给“能力”列的小图标用），按固定顺序；后端读不到插件版本时为空 */
const channelKinds = (channel: ChannelListItem) =>
  PLUGIN_KIND_ORDER.filter((kind) => channel.kinds.includes(kind));

/** 渠道页的表格：搜索、筛选、分页都在服务端，行内图标操作加「⋯」菜单；点整行打开渠道弹窗 */
export function ChannelTable({
  list,
  plugins,
  canWrite,
  onOpen,
  onNew,
  onImport,
  onSetKey,
  onToggle,
  onDelete,
}: {
  /** 渠道分页列表（useAdminList）：当页的行、总数、搜索 / 筛选 / 翻页 */
  list: ReturnType<typeof useAdminList<ChannelListItem>>;
  /** 全部插件：插件筛选下拉和“插件”列显示名要用 */
  plugins: PluginView[];
  canWrite: boolean;
  onOpen: (key: string) => void;
  onNew: () => void;
  onImport: (channel: ChannelListItem) => void;
  onSetKey: (channel: ChannelListItem) => void;
  onToggle: (channel: ChannelListItem) => void;
  onDelete: (channel: ChannelListItem) => void;
}) {
  const { items: rows, state: status } = list;
  const kind = list.filters.kind ?? "";
  const columns = 7;

  if (status === "ready" && list.total === 0 && !list.filtering) {
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
    <DataPanel>
      <TableToolbar>
        <ListSearch
          value={list.q}
          onCommit={list.setQ}
          placeholder="搜索名称、key 或地址"
          className="[&_input]:h-8"
        />
        <KindFilter
          value={kind}
          onChange={(value) => list.setFilter("kind", value)}
          slideId="channel-kind"
        />
        <FilterSelect
          aria-label="按插件筛选"
          value={list.filters.plugin ?? ""}
          onValueChange={(value) => list.setFilter("plugin", value)}
          options={[["", "全部插件"], ...plugins.map((item) => [item.key, item.name] as const)]}
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
              <TableHead className="text-muted-foreground px-3 text-xs">渠道</TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">插件</TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs max-md:hidden">
                能力
              </TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">模型</TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">状态</TableHead>
              <TableHead className="text-muted-foreground px-3 text-xs">启用</TableHead>
              <TableHead className="text-muted-foreground px-3 text-right text-xs">操作</TableHead>
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
                  没有匹配的渠道
                </TableCell>
              </TableRow>
            )}
            {status === "ready" &&
              rows.map((channel) => {
                const owner = plugins.find((item) => item.key === channel.plugin_key);
                const health = channel.health;
                const upgrade = channel.upgrade_to;
                const importUnsupported = !!owner && !importable(owner, channel);
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
                        <div className="text-muted-foreground text-xs">可升级到 v{upgrade}</div>
                      )}
                    </TableCell>
                    <TableCell className="px-3 py-3 max-md:hidden">
                      <KindIcons kinds={channelKinds(channel)} />
                    </TableCell>
                    <TableCell className="px-3 py-3 tabular-nums">{channel.model_count}</TableCell>
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
