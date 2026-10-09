import { CloudOff, RotateCw } from "lucide-react";

import {
  EmptyState,
  EmptyStateActions,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { Button } from "@/components/ui/button";

/**
 * 图表卡的“加载失败”占位：统计接口失败时只有依赖它的卡片换成这个，配置类的指标不受影响。
 * @param title 失败说明，如“任务统计加载失败”
 * @param onRetry 点击“重试”
 */
export function ChartError({ title, onRetry }: { title: string; onRetry: () => void }) {
  return (
    <EmptyState className="h-full min-h-48 border-0">
      <EmptyStateIcon>
        <CloudOff />
      </EmptyStateIcon>
      <EmptyStateTitle>{title}</EmptyStateTitle>
      <EmptyStateActions>
        <Button size="sm" variant="outline" onClick={onRetry}>
          <RotateCw />
          重试
        </Button>
      </EmptyStateActions>
    </EmptyState>
  );
}
