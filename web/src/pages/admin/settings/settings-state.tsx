import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";

/** 设置页加载中的骨架 */
export function SettingsSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      <Skeleton className="h-40" />
      <Skeleton className="h-32" />
    </div>
  );
}

/**
 * 设置页加载失败：给一个重试按钮
 * @param onRetry 重试
 */
export function SettingsError({ onRetry }: { onRetry: () => void }) {
  return (
    <EmptyState>
      <EmptyStateTitle className="text-destructive">加载失败</EmptyStateTitle>
      <EmptyStateActions>
        <Button variant="outline" size="sm" onClick={onRetry}>
          重试
        </Button>
      </EmptyStateActions>
    </EmptyState>
  );
}
