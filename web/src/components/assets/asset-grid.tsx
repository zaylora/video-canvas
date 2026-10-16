import type { ReactNode } from "react";
import { Loader2 } from "lucide-react";
import { useEffect, useRef } from "react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/** 素材网格：手机两列，桌面按最小 180px 自适应铺开 */
export const ASSET_GRID =
  "grid grid-cols-2 gap-2.5 md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))]";

/**
 * 素材网格容器
 * @param children 卡片
 */
export function AssetGrid({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div data-slot="asset-grid" className={cn(ASSET_GRID, className)}>
      {children}
    </div>
  );
}

/**
 * 首屏加载的骨架卡片，尺寸和真卡一致
 * @param count 骨架数量
 */
export function AssetGridSkeleton({ count = 12 }: { count?: number }) {
  return (
    <div aria-busy data-slot="asset-grid-skeleton" className={ASSET_GRID}>
      {Array.from({ length: count }, (_, i) => (
        <Skeleton key={i} className="aspect-square rounded-xl motion-reduce:animate-none" />
      ))}
    </div>
  );
}

/**
 * 列表底部：滚动到它进入视野（提前 400px）就触发加载下一页；加载中显示小转圈，
 * 下一页失败时停止自动加载，显示「重试」由用户决定。没有更多时不渲染。
 * @param hasMore 是否还有下一页
 * @param loading 正在加载下一页
 * @param failed 下一页加载失败过
 * @param onLoadMore 加载下一页
 */
export function LoadMoreFooter({
  hasMore,
  loading,
  failed,
  onLoadMore,
}: {
  hasMore: boolean;
  loading: boolean;
  failed: boolean;
  onLoadMore: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  /** 回调放 ref：观察器只在条件变化时重建，不因每次渲染的新函数反复订阅 */
  const loadRef = useRef(onLoadMore);
  useEffect(() => {
    loadRef.current = onLoadMore;
  });

  const watching = hasMore && !loading && !failed;
  useEffect(() => {
    const node = ref.current;
    if (!watching || !node) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) loadRef.current();
      },
      { rootMargin: "400px 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [watching]);

  if (!hasMore) return null;
  return (
    <div
      ref={ref}
      data-slot="load-more"
      className="text-muted-foreground flex h-16 items-center justify-center gap-2 text-[13px]"
    >
      {loading && <Loader2 aria-label="加载中" className="size-4 animate-spin" />}
      {failed && (
        <>
          加载失败
          <Button variant="outline" size="xs" onClick={onLoadMore}>
            重试
          </Button>
        </>
      )}
    </div>
  );
}
