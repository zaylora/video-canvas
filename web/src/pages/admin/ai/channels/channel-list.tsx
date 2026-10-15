import { useState } from "react";
import { Reorder, useDragControls } from "motion/react";
import { ChevronRight, GripVertical, Plus } from "lucide-react";

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
 * 主从页左栏：渠道标题与数量、新增渠道、搜索、状态筛选、渠道列表。
 * - 新增渠道、拖动排序只对 super_admin 开放；admin 看到的新增按钮置灰并写明原因，不隐藏；
 * - 拖动排序：每项左侧的手柄，拖动时只改本地顺序，松手才提交（onReorder）；服务端的列表变了就以服务端为准；
 *   搜索或筛选时列表只是子集，不能拖（否则无法得到完整的新顺序）；
 * - 列表顺序就是渠道顺序（用户在画布里看到的模型顺序按它排）。
 * @param channels 全部渠道（渠道顺序）
 * @param status 渠道清单的加载状态
 * @param selectedKey 当前选中的渠道 key
 * @param modelCounts 各渠道的模型数
 * @param query 搜索词
 * @param statusFilter 状态筛选
 * @param canWrite 是否 super_admin
 * @param onReorder 松手后顺序变了就提交完整的新顺序（渠道 key 列表）
 */
export function ChannelList({
  channels,
  status,
  selectedKey,
  modelCounts,
  query,
  statusFilter,
  canWrite,
  onSelect,
  onNew,
  onQueryChange,
  onStatusChange,
  onReorder,
  onRetry,
}: {
  channels: ChannelOption[];
  status: LoadStatus;
  selectedKey: string | null;
  modelCounts: Record<string, number>;
  query: string;
  statusFilter: ChannelStatusFilter;
  canWrite: boolean;
  onSelect: (key: string) => void;
  onNew: () => void;
  onQueryChange: (value: string) => void;
  onStatusChange: (value: ChannelStatusFilter) => void;
  onReorder: (keys: string[]) => void;
  onRetry: () => void;
}) {
  const visible = channels.filter((channel) => matchChannel(channel, query, statusFilter));
  const reason = canWrite ? null : SUPER_ONLY;
  /** 看到的就是全集才能拖：搜索 / 筛选中只是子集 */
  const draggable = canWrite && channels.length > 1 && visible.length === channels.length;
  const [local, setLocal] = useState(channels);
  const [prevChannels, setPrevChannels] = useState(channels);
  /** 服务端的列表变了就重置本地顺序（在渲染里同步，免得闪一帧旧顺序） */
  if (channels !== prevChannels) {
    setPrevChannels(channels);
    setLocal(channels);
  }
  /** 松手：顺序真的变了才提交 */
  const commit = () => {
    if (local.some((channel, index) => channel.key !== channels[index]?.key)) {
      onReorder(local.map((channel) => channel.key));
    }
  };

  return (
    <aside
      data-slot="channel-list"
      className="bg-card flex min-w-0 flex-col gap-3 rounded-xl border p-3 lg:min-h-0"
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
        onChange={(event) => onQueryChange(event.target.value)}
      />

      <Segmented aria-label="渠道状态" className="w-full [&>button]:flex-1">
        {STATUS_OPTIONS.map(([value, label]) => (
          <SegmentedItem
            key={value}
            slideId="channel-status"
            active={statusFilter === value}
            onClick={() => onStatusChange(value)}
          >
            {label}
          </SegmentedItem>
        ))}
      </Segmented>

      <div className="flex max-h-72 min-h-0 flex-col gap-2 overflow-y-auto lg:max-h-none lg:min-h-24 lg:flex-1">
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
        ) : draggable ? (
          <Reorder.Group
            as="div"
            axis="y"
            values={local}
            onReorder={setLocal}
            className="flex flex-col gap-2"
          >
            {local.map((channel) => (
              <ChannelItem
                key={channel.key}
                channel={channel}
                modelCount={modelCounts[channel.key] ?? 0}
                selected={channel.key === selectedKey}
                draggable
                onDragEnd={commit}
                onSelect={() => onSelect(channel.key)}
              />
            ))}
          </Reorder.Group>
        ) : (
          visible.map((channel) => (
            <ChannelItem
              key={channel.key}
              channel={channel}
              modelCount={modelCounts[channel.key] ?? 0}
              selected={channel.key === selectedKey}
              onSelect={() => onSelect(channel.key)}
            />
          ))
        )}
      </div>
    </aside>
  );
}

/** 一个渠道项：（可拖动时）左侧拖动手柄、名称、“N 个模型 · ● 状态”。只有手柄能拖，免得点选时误触发排序 */
function ChannelItem({
  channel,
  modelCount,
  selected,
  draggable = false,
  onDragEnd,
  onSelect,
}: {
  channel: ChannelOption;
  modelCount: number;
  selected: boolean;
  draggable?: boolean;
  onDragEnd?: () => void;
  onSelect: () => void;
}) {
  const controls = useDragControls();
  const body = (
    <>
      {draggable && (
        <button
          type="button"
          aria-label={`拖动排序 ${channel.name}`}
          title="拖动排序"
          onPointerDown={(event) => controls.start(event)}
          className="text-muted-foreground hover:bg-background hover:text-foreground focus-visible:ring-ring/50 grid h-8 w-5 shrink-0 cursor-grab touch-none place-items-center rounded-md outline-none focus-visible:ring-2 active:cursor-grabbing"
        >
          <GripVertical className="size-4" />
        </button>
      )}
      <button
        type="button"
        aria-current={selected ? "true" : undefined}
        onClick={onSelect}
        className="focus-visible:ring-ring/50 flex min-w-0 flex-1 items-center gap-1 rounded-md text-left outline-none focus-visible:ring-3"
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
        <ChevronRight className="text-muted-foreground size-4 shrink-0" />
      </button>
    </>
  );
  const className = cn(
    "bg-card flex items-center gap-1 rounded-lg border px-2.5 py-2 transition-colors",
    selected ? "border-primary/60 bg-primary/5" : "hover:bg-muted/60",
  );
  if (!draggable) return <div className={className}>{body}</div>;
  return (
    <Reorder.Item
      as="div"
      value={channel}
      dragListener={false}
      dragControls={controls}
      onDragEnd={onDragEnd}
      className={className}
    >
      {body}
    </Reorder.Item>
  );
}
