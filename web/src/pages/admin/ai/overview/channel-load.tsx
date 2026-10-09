import { ArrowRight, Plus, RadioTower } from "lucide-react";
import { useMemo } from "react";

import type { ChannelLoad as ChannelLoadDto, ChannelView, PluginView } from "@/api/admin/ai/type";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { LoadBar } from "@/components/admin-ui/load-bar";
import { Tag, type TagTone } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { HEALTH_UI_TONE } from "@/utils/admin/health";
import { loadRows, type LoadRow } from "@/utils/admin/overview-stats";

import type { LoadStatus } from "../../use-admin";
import { ChartError } from "./chart-states";

/** 最多列出几行，其余去渠道页看 */
const MAX_ROWS = 6;

/** 一行右侧的文字：不可用写原因，空闲写“空闲”，否则是 生成中 / 上限 + 排队 / 满载标签 */
function RowStatus({ row }: { row: LoadRow }) {
  if (!row.usable) return <Tag tone={HEALTH_UI_TONE[row.tone] as TagTone}>{row.label}</Tag>;
  if (row.idle) return <span>空闲</span>;
  return (
    <>
      <span>
        <b className="text-foreground font-medium tabular-nums">{row.running}</b> /{" "}
        {row.max || "不限"}
      </span>
      {row.waiting > 0 && (
        <Tag tone="info" className="tabular-nums">
          排队 {row.waiting}
        </Tag>
      )}
      {row.full && row.waiting === 0 && <Tag tone="warning">满载</Tag>}
    </>
  );
}

/**
 * 渠道负载（实时）：每个渠道一条负载条，生成中 / 排队 / 上限一眼看清。
 * - 不可用的渠道（缺 Key、停用）不画负载，写原因，排在最后；最多列 6 行，其余去渠道页；
 * - 负载接口失败时：已有数据就保留并提示“显示的是旧数据”，从没成功过才整卡显示失败；
 * - 没有渠道时引导新建（只有运维能写）。
 * @param channels 渠道清单
 * @param plugins 插件清单
 * @param catalogReady 渠道与插件清单都已加载
 * @param loads 各渠道负载
 * @param status 负载的加载状态
 * @param stale 最近一次刷新失败
 * @param onRetry 失败后的重试
 * @param onOpenChannels 去渠道页
 * @param onCreateChannel 新建渠道；没有权限时为 null
 */
export function ChannelLoad({
  channels,
  plugins,
  catalogReady,
  loads,
  status,
  stale,
  onRetry,
  onOpenChannels,
  onCreateChannel,
}: {
  channels: ChannelView[];
  plugins: PluginView[];
  catalogReady: boolean;
  loads: ChannelLoadDto[];
  status: LoadStatus;
  stale: boolean;
  onRetry: () => void;
  onOpenChannels: () => void;
  onCreateChannel: (() => void) | null;
}) {
  const rows = useMemo(() => loadRows(channels, plugins, loads), [channels, plugins, loads]);
  const loading = !catalogReady || status === "loading";

  return (
    <Card data-slot="channel-load" className="h-full">
      <CardHeader>
        <CardTitle>渠道负载</CardTitle>
        <CardDescription className="text-xs">
          生成中 / 同时生成上限，排队的任务叠在后面
        </CardDescription>
        <CardAction>
          <span
            className={cn(
              "text-muted-foreground flex items-center gap-1.5 text-xs",
              stale && "text-amber-600 dark:text-amber-400",
            )}
          >
            <span
              aria-hidden
              className={cn("size-1.5 rounded-full", stale ? "bg-amber-500" : "bg-status-success")}
            />
            {stale ? "刷新失败，显示的是旧数据" : "每 15 秒刷新"}
          </span>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-3.5">
        {loading ? (
          <>
            <Skeleton className="h-5" />
            <Skeleton className="h-5" />
            <Skeleton className="h-5" />
          </>
        ) : status === "error" ? (
          <ChartError title="渠道负载加载失败" onRetry={onRetry} />
        ) : rows.length === 0 ? (
          <EmptyState className="flex-1 border-0">
            <EmptyStateIcon>
              <RadioTower />
            </EmptyStateIcon>
            <EmptyStateTitle>还没有渠道</EmptyStateTitle>
            {onCreateChannel && (
              <EmptyStateActions>
                <Button size="sm" variant="outline" onClick={onCreateChannel}>
                  <Plus />
                  新建渠道
                </Button>
              </EmptyStateActions>
            )}
          </EmptyState>
        ) : (
          <>
            <ul className="flex flex-col gap-3.5">
              {rows.slice(0, MAX_ROWS).map((row) => (
                <li
                  key={row.key}
                  className="grid grid-cols-[5.5rem_minmax(0,1fr)_auto] items-center gap-3 text-sm sm:grid-cols-[7rem_minmax(0,1fr)_auto]"
                >
                  <div className={cn("min-w-0", !row.usable && "text-muted-foreground")}>
                    <div className="truncate">{row.name}</div>
                    <div className="text-muted-foreground truncate text-[11px]">{row.key}</div>
                  </div>
                  <LoadBar
                    running={row.runningRatio}
                    queued={row.queuedRatio}
                    full={row.full}
                    className={cn(!row.usable && "opacity-50")}
                    label={
                      row.usable
                        ? `${row.name}：生成中 ${row.running}，排队 ${row.waiting}，上限 ${row.max || "不限"}`
                        : `${row.name}：${row.label}`
                    }
                  />
                  <div className="text-muted-foreground flex min-w-24 items-center justify-end gap-2 text-xs whitespace-nowrap">
                    <RowStatus row={row} />
                  </div>
                </li>
              ))}
            </ul>
            <Button
              size="sm"
              variant="ghost"
              className="-ml-2 mt-auto self-start"
              onClick={onOpenChannels}
            >
              全部 {rows.length} 个渠道
              <ArrowRight />
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}
