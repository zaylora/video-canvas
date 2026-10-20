import { useMemo } from "react";

import type { ThroughputPoint } from "@/api/admin/queue/type";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  formatClock,
  formatDuration,
  formatPercent,
  summarizeThroughput,
  throughputBars,
} from "@/utils/admin/queue";

import type { LoadStatus } from "../../use-admin";
import { ChartError } from "../overview/chart-states";

/** 柱状图高度（px） */
const CHART_HEIGHT = 96;

/**
 * 近 1 小时曲线：按分钟的完成数 / 失败数（堆叠柱），摘要显示完成数、失败数、失败率、平均排队时长。
 * 柱子一次性绘制、不做动画（每 5 秒刷新一次数据）；颜色走状态 token。
 * @param points 按分钟的数据，加载中为 null
 * @param status 加载状态
 * @param stale 最近一次刷新失败
 * @param onRetry 失败后的重试
 */
export function ThroughputCard({
  points,
  status,
  stale,
  onRetry,
}: {
  points: ThroughputPoint[] | null;
  status: LoadStatus;
  stale: boolean;
  onRetry: () => void;
}) {
  const summary = useMemo(() => summarizeThroughput(points ?? []), [points]);
  const bars = useMemo(() => throughputBars(points ?? []), [points]);

  return (
    <Card data-slot="throughput-card" size="sm">
      <CardHeader>
        <CardTitle>近 1 小时</CardTitle>
        <CardDescription className="text-xs tabular-nums">
          {points
            ? `完成 ${summary.succeeded} · 失败 ${summary.failed}（${formatPercent(summary.failureRate)}）· 平均排队 ${formatDuration(summary.avgWaitMs / 1000)}`
            : " "}
        </CardDescription>
        <CardAction>
          <ul className="text-muted-foreground flex gap-3 text-xs" aria-hidden>
            <li className="flex items-center gap-1.5">
              <span className="bg-status-success size-2 rounded-[2px]" />
              完成
            </li>
            <li className="flex items-center gap-1.5">
              <span className="bg-destructive size-2 rounded-[2px]" />
              失败
            </li>
          </ul>
        </CardAction>
      </CardHeader>
      <CardContent>
        {status === "error" && !points ? (
          <ChartError title="吞吐曲线加载失败" onRetry={onRetry} />
        ) : !points ? (
          <Skeleton style={{ height: CHART_HEIGHT }} />
        ) : summary.empty ? (
          <p className="text-muted-foreground py-6 text-center text-sm">最近 1 小时没有任务。</p>
        ) : (
          <>
            <div
              role="img"
              aria-label={`近 1 小时按分钟的完成数与失败数：完成 ${summary.succeeded}，失败 ${summary.failed}`}
              className="flex items-end gap-0.5"
              style={{ height: CHART_HEIGHT }}
            >
              {bars.map((bar) => (
                <div
                  key={bar.minute}
                  title={`${formatClock(bar.minute)} 完成 ${bar.succeeded} · 失败 ${bar.failed}`}
                  className="flex h-full min-w-0 flex-1 flex-col-reverse"
                >
                  <span
                    className="bg-status-success/60 block rounded-t-[2px]"
                    style={{ height: `${bar.succeededRatio * 100}%` }}
                  />
                  <span
                    className="bg-destructive block"
                    style={{ height: `${bar.failedRatio * 100}%` }}
                  />
                </div>
              ))}
            </div>
            <div className="text-muted-foreground mt-1 flex justify-between text-[11px]">
              <span>{bars.length} 分钟前</span>
              <span>现在</span>
            </div>
          </>
        )}
        {stale && points && (
          <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
            刷新失败，显示的是旧数据
          </p>
        )}
      </CardContent>
    </Card>
  );
}
