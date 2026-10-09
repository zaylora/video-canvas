import { useMemo, useState } from "react";
import { Pie, PieChart, Sector } from "recharts";
import type { PieSectorShapeProps } from "recharts";

import type { AdminStats } from "@/api/admin/ai/type";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { topN, type ShareItem } from "@/utils/admin/overview-stats";

import type { LoadStatus } from "../../use-admin";
import { ChartError } from "./chart-states";

/** 占比维度 */
type ShareBy = "model" | "kind";

/** 类别色：前 5 名按顺序取，“其他”用中性色 */
const PALETTE = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
];
const colorOf = (item: ShareItem, index: number) =>
  item.other ? "var(--chart-other)" : PALETTE[index % PALETTE.length];

/** 占比图不用 ChartConfig 绑色（每片的色在数据里），给容器一个空配置 */
const emptyConfig = {} satisfies ChartConfig;

/**
 * 调用占比（环形图 + 图例）：
 * - 可按模型（前 5 名 + 其他）或按任务类型切换；
 * - 悬停环片或图例行：其余变淡，中心换成该项的次数、名称与占比；图例行可聚焦，键盘等价于悬停；
 * - 环按“范围 + 维度”加 key，切换时重新挂载、重播 index.css 的 chart-donut-in 入场。
 * @param stats 当前范围的统计，加载中为 null
 * @param status 统计的加载状态
 * @param onRetry 失败后的重试
 */
export function ShareDonut({
  stats,
  status,
  onRetry,
}: {
  stats: AdminStats | null;
  status: LoadStatus;
  onRetry: () => void;
}) {
  const [by, setBy] = useState<ShareBy>("model");
  const [hovered, setHovered] = useState<number | null>(null);

  const items = useMemo<ShareItem[]>(() => {
    if (!stats) return [];
    return by === "model"
      ? topN(
          stats.by_model.map((m) => ({ key: m.model, label: m.label, count: m.count })),
          5,
        )
      : stats.by_kind.map((k) => ({
          key: k.kind,
          label: MODEL_KIND_LABEL[k.kind] ?? k.kind,
          count: k.count,
        }));
  }, [stats, by]);
  const total = items.reduce((sum, item) => sum + item.count, 0);
  const active = hovered !== null ? items[hovered] : undefined;

  const changeBy = (next: ShareBy) => {
    setBy(next);
    setHovered(null);
  };

  return (
    <Card data-slot="share-donut">
      <CardHeader>
        <CardTitle>调用占比</CardTitle>
        <CardDescription className="text-xs">
          {stats ? `近 ${stats.days} 天 · ${by === "model" ? "前 5 名" : "按生成类型"}` : " "}
        </CardDescription>
        <CardAction>
          <Segmented aria-label="占比维度">
            <SegmentedItem
              slideId="overview-share-by"
              active={by === "model"}
              className="px-2.5 py-1"
              onClick={() => changeBy("model")}
            >
              模型
            </SegmentedItem>
            <SegmentedItem
              slideId="overview-share-by"
              active={by === "kind"}
              className="px-2.5 py-1"
              onClick={() => changeBy("kind")}
            >
              类型
            </SegmentedItem>
          </Segmented>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-3">
        {status === "error" ? (
          <ChartError title="占比统计加载失败" onRetry={onRetry} />
        ) : !stats ? (
          <>
            <Skeleton className="mx-auto size-37 rounded-full" />
            <Skeleton className="h-24" />
          </>
        ) : (
          <>
            <div className="relative mx-auto size-37">
              <ChartContainer
                key={`${stats.days}-${by}`}
                config={emptyConfig}
                role="img"
                aria-label={`近 ${stats.days} 天调用占比环形图`}
                className="chart-donut-in aspect-square size-full"
              >
                <PieChart>
                  <Pie
                    data={total === 0 ? [{ key: "empty", label: "", count: 1 }] : items}
                    dataKey="count"
                    nameKey="label"
                    innerRadius="68%"
                    outerRadius="100%"
                    paddingAngle={items.length > 1 ? 2 : 0}
                    stroke="none"
                    isAnimationActive={false}
                    onMouseEnter={(_, index) => setHovered(index)}
                    onMouseLeave={() => setHovered(null)}
                    shape={(props: PieSectorShapeProps) => (
                      <Sector
                        {...props}
                        fill={
                          total === 0 ? "var(--border)" : colorOf(items[props.index], props.index)
                        }
                        opacity={hovered === null || hovered === props.index ? 1 : 0.3}
                        className="transition-opacity duration-120"
                      />
                    )}
                  />
                </PieChart>
              </ChartContainer>
              <div
                aria-live="polite"
                className="pointer-events-none absolute inset-0 grid place-content-center px-9 text-center"
              >
                <div className="text-2xl leading-tight font-semibold tabular-nums">
                  {(active ? active.count : total).toLocaleString()}
                </div>
                <div className="text-muted-foreground truncate text-xs">
                  {active
                    ? `${active.label} · ${((active.count / total) * 100).toFixed(1)}%`
                    : "次调用"}
                </div>
              </div>
            </div>

            {total === 0 ? (
              <p className="text-muted-foreground py-4 text-center text-sm">这段时间还没有任务</p>
            ) : (
              <ul className="-mx-1.5 flex flex-col gap-0.5">
                {items.map((item, index) => (
                  <li key={item.key}>
                    <button
                      type="button"
                      onMouseEnter={() => setHovered(index)}
                      onMouseLeave={() => setHovered(null)}
                      onFocus={() => setHovered(index)}
                      onBlur={() => setHovered(null)}
                      className={cn(
                        "hover:bg-accent focus-visible:ring-ring flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-left text-sm transition-[background-color,opacity] focus-visible:ring-2 focus-visible:outline-none",
                        hovered !== null && hovered !== index && "opacity-45",
                      )}
                    >
                      <span
                        className="size-2 shrink-0 rounded-[2px]"
                        style={{ background: colorOf(item, index) }}
                      />
                      <span className="min-w-0 flex-1 truncate">{item.label}</span>
                      <span className="text-muted-foreground tabular-nums">
                        {item.count.toLocaleString()}
                      </span>
                      <span className="w-11 text-right font-medium tabular-nums">
                        {((item.count / total) * 100).toFixed(1)}%
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
