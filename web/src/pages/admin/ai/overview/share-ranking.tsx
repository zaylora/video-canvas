import { motion } from "motion/react";
import { useMemo, useState } from "react";

import type { AdminStats } from "@/api/admin/ai/type";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { topN, type ShareItem } from "@/utils/admin/overview-stats";

import type { LoadStatus } from "../../use-admin";
import { ChartError } from "./chart-states";
import { GLASS_PANEL, GlassSegmented } from "@/components/admin-ui/glass";

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

/**
 * 调用排行：按模型（前 5 名 + 其他）或按任务类型，每行一条横向占比胶囊条。
 * - 头部是区间内的总调用次数；
 * - 条长只用 scaleX（origin-left）+ SPRING 过渡，切范围或维度时从当前长度滑到新长度。
 * @param stats 当前范围的统计，加载中为 null
 * @param status 统计的加载状态
 * @param onRetry 失败后的重试
 */
export function ShareRanking({
  stats,
  status,
  onRetry,
  className,
}: {
  stats: AdminStats | null;
  status: LoadStatus;
  onRetry: () => void;
  className?: string;
}) {
  const [by, setBy] = useState<ShareBy>("model");

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
  const top = Math.max(0, ...items.map((item) => item.count));

  return (
    <Card data-slot="share-ranking" className={cn(GLASS_PANEL, "gap-5 py-5", className)}>
      <CardHeader className="px-5">
        <CardTitle>调用排行</CardTitle>
        <CardDescription className="text-xs tabular-nums">
          {stats ? `近 ${stats.days} 天 · 共 ${total.toLocaleString()} 次` : " "}
        </CardDescription>
        <CardAction>
          <GlassSegmented
            id="overview-share-by"
            label="排行维度"
            size="sm"
            value={by}
            onValueChange={setBy}
            items={[
              { value: "model", label: "模型" },
              { value: "kind", label: "类型" },
            ]}
          />
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col px-5">
        {status === "error" ? (
          <ChartError title="调用统计加载失败" onRetry={onRetry} />
        ) : !stats ? (
          <div className="flex flex-col gap-4">
            <Skeleton className="h-8 rounded-full" />
            <Skeleton className="h-8 rounded-full" />
            <Skeleton className="h-8 rounded-full" />
          </div>
        ) : total === 0 ? (
          <p className="text-muted-foreground m-auto py-10 text-sm">这段时间还没有任务</p>
        ) : (
          <ul className="flex flex-col gap-4">
            {items.map((item, index) => (
              <li key={item.key} className="flex flex-col gap-1.5">
                <div className="flex items-baseline gap-2 text-sm">
                  <span className="min-w-0 flex-1 truncate">{item.label}</span>
                  <span className="text-muted-foreground text-xs tabular-nums">
                    {item.count.toLocaleString()}
                  </span>
                  <span className="w-12 text-right font-medium tabular-nums">
                    {((item.count / total) * 100).toFixed(1)}%
                  </span>
                </div>
                <div
                  data-slot="share-bar"
                  className="bg-chrome-hover h-1.5 overflow-hidden rounded-full"
                >
                  <motion.span
                    initial={{ scaleX: 0 }}
                    animate={{ scaleX: item.count / top }}
                    transition={SPRING}
                    className="block h-full origin-left rounded-full"
                    style={{ background: colorOf(item, index) }}
                  />
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
