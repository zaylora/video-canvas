import { ArrowDown, ArrowUp, ChevronRight, ListOrdered, Loader2, Plus, Check } from "lucide-react";

import type { ChannelOption } from "@/api/admin/ai/type.d";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { SearchInput } from "@/components/admin-ui/search-input";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusLabel } from "@/components/admin-ui/status-dot";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { ChannelStatusFilter } from "@/utils/admin/channel-route";

import type { LoadStatus } from "../../use-admin";

/** 非 super_admin 点不了写操作时的统一原因 */
export const SUPER_ONLY = "仅 super_admin 可用";

/** 状态筛选的选项 */
const STATUS_OPTIONS: ReadonlyArray<readonly [ChannelStatusFilter, string]> = [
  ["all", "全部"],
  ["on", "已启用"],
  ["off", "已停用"],
];

/** 渠道是否命中左栏的搜索词与状态筛选：搜名称、key、地址；状态按启用与否 */
export function matchChannel(
  channel: ChannelOption,
  query: string,
  status: ChannelStatusFilter,
): boolean {
  if (status === "on" && !channel.enabled) return false;
  if (status === "off" && channel.enabled) return false;
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return `${channel.name} ${channel.base_url} ${channel.key}`.toLowerCase().includes(q);
}

/**
 * 主从页左栏：渠道标题与数量、新增渠道、搜索、状态筛选、渠道列表、设置排序。
 * - 新增渠道、设置排序只对 super_admin 开放；admin 看到的是置灰按钮并写明原因，不隐藏；
 * - 排序模式下列表显示全部渠道（忽略搜索与筛选），每项带上移 / 下移，点选渠道暂时不可用；
 * - 列表顺序就是渠道顺序（用户在画布里看到的模型顺序按它排）。
 * @param channels 全部渠道（渠道顺序）
 * @param status 渠道清单的加载状态
 * @param selectedKey 当前选中的渠道 key
 * @param modelCounts 各渠道的模型数
 * @param query 搜索词
 * @param statusFilter 状态筛选
 * @param canWrite 是否 super_admin
 * @param sorting 是否处于排序模式
 * @param sortOrder 排序模式下的渠道 key 顺序（草稿）
 * @param savingOrder 正在保存排序
 */
export function ChannelList({
  channels,
  status,
  selectedKey,
  modelCounts,
  query,
  statusFilter,
  canWrite,
  sorting,
  sortOrder,
  savingOrder,
  onSelect,
  onNew,
  onQueryChange,
  onStatusChange,
  onToggleSorting,
  onMove,
  onRetry,
}: {
  channels: ChannelOption[];
  status: LoadStatus;
  selectedKey: string | null;
  modelCounts: Record<string, number>;
  query: string;
  statusFilter: ChannelStatusFilter;
  canWrite: boolean;
  sorting: boolean;
  sortOrder: string[];
  savingOrder: boolean;
  onSelect: (key: string) => void;
  onNew: () => void;
  onQueryChange: (value: string) => void;
  onStatusChange: (value: ChannelStatusFilter) => void;
  onToggleSorting: () => void;
  onMove: (key: string, delta: -1 | 1) => void;
  onRetry: () => void;
}) {
  const byKey = new Map(channels.map((channel) => [channel.key, channel]));
  const visible = sorting
    ? sortOrder.flatMap((key) => byKey.get(key) ?? [])
    : channels.filter((channel) => matchChannel(channel, query, statusFilter));
  const reason = canWrite ? null : SUPER_ONLY;

  return (
    <aside
      data-slot="channel-list"
      className="bg-card flex min-w-0 flex-col gap-3 rounded-xl border p-3 lg:self-start"
    >
      <div className="flex items-center gap-2">
        <h2 className="text-sm font-semibold">渠道</h2>
        <span className="bg-muted text-muted-foreground rounded-md px-1.5 text-xs tabular-nums">
          {channels.length}
        </span>
      </div>

      <ReasonTooltip reason={reason} className="w-full">
        <Button className="w-full" disabled={!canWrite} onClick={onNew}>
          <Plus />
          新增渠道
        </Button>
      </ReasonTooltip>

      <SearchInput
        aria-label="搜索渠道名称或地址"
        placeholder="搜索渠道名称或地址"
        autoComplete="off"
        value={query}
        disabled={sorting}
        onChange={(event) => onQueryChange(event.target.value)}
      />

      <Segmented aria-label="渠道状态" className="w-full [&>button]:flex-1">
        {STATUS_OPTIONS.map(([value, label]) => (
          <SegmentedItem
            key={value}
            slideId="channel-status"
            active={statusFilter === value}
            disabled={sorting}
            onClick={() => onStatusChange(value)}
          >
            {label}
          </SegmentedItem>
        ))}
      </Segmented>

      <div className="flex max-h-72 min-h-0 flex-col gap-2 overflow-y-auto lg:max-h-[calc(100svh-26rem)] lg:min-h-24">
        {status === "loading" ? (
          <>
            <Skeleton className="h-14" />
            <Skeleton className="h-14" />
            <Skeleton className="h-14" />
          </>
        ) : status === "error" && channels.length === 0 ? (
          <div className="text-muted-foreground flex flex-col items-start gap-2 p-2 text-xs">
            渠道清单加载失败
            <Button size="xs" variant="outline" onClick={onRetry}>
              重试
            </Button>
          </div>
        ) : visible.length === 0 ? (
          <p className="text-muted-foreground p-2 text-xs">
            {channels.length ? "没有匹配的渠道" : "还没有渠道"}
          </p>
        ) : (
          visible.map((channel, index) => (
            <ChannelItem
              key={channel.key}
              channel={channel}
              modelCount={modelCounts[channel.key] ?? 0}
              selected={channel.key === selectedKey}
              sorting={sorting}
              first={index === 0}
              last={index === visible.length - 1}
              busy={savingOrder}
              onSelect={() => onSelect(channel.key)}
              onMove={(delta) => onMove(channel.key, delta)}
            />
          ))
        )}
      </div>

      <ReasonTooltip reason={reason} className="mt-auto w-full">
        <Button
          variant="outline"
          className="w-full"
          disabled={!canWrite || savingOrder || (!sorting && channels.length < 2)}
          onClick={onToggleSorting}
        >
          {savingOrder ? (
            <Loader2 className="animate-spin" />
          ) : sorting ? (
            <Check />
          ) : (
            <ListOrdered />
          )}
          {sorting ? "完成排序" : "设置排序"}
        </Button>
      </ReasonTooltip>
    </aside>
  );
}

/** 一个渠道项：名称、“N 个模型 · ● 状态”；排序模式下右侧是上移 / 下移 */
function ChannelItem({
  channel,
  modelCount,
  selected,
  sorting,
  first,
  last,
  busy,
  onSelect,
  onMove,
}: {
  channel: ChannelOption;
  modelCount: number;
  selected: boolean;
  sorting: boolean;
  first: boolean;
  last: boolean;
  busy: boolean;
  onSelect: () => void;
  onMove: (delta: -1 | 1) => void;
}) {
  return (
    <div
      className={cn(
        "flex items-center gap-1 rounded-lg border px-2.5 py-2 transition-colors",
        selected ? "border-primary/60 bg-primary/5" : "hover:bg-muted/60",
      )}
    >
      <button
        type="button"
        aria-current={selected ? "true" : undefined}
        disabled={sorting}
        onClick={onSelect}
        className="focus-visible:ring-ring/50 flex min-w-0 flex-1 items-center gap-1 rounded-md text-left outline-none focus-visible:ring-3 disabled:cursor-default"
      >
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">{channel.name}</span>
          <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
            {modelCount} 个模型 ·
            <StatusLabel tone={channel.enabled ? "success" : "danger"}>
              {channel.enabled ? "已启用" : "已停用"}
            </StatusLabel>
          </span>
        </span>
        {!sorting && <ChevronRight className="text-muted-foreground size-4 shrink-0" />}
      </button>
      {sorting && (
        <span className="flex shrink-0 gap-1">
          <Button
            size="icon-xs"
            variant="outline"
            aria-label={`上移 ${channel.name}`}
            disabled={first || busy}
            onClick={() => onMove(-1)}
          >
            <ArrowUp />
          </Button>
          <Button
            size="icon-xs"
            variant="outline"
            aria-label={`下移 ${channel.name}`}
            disabled={last || busy}
            onClick={() => onMove(1)}
          >
            <ArrowDown />
          </Button>
        </span>
      )}
    </div>
  );
}
