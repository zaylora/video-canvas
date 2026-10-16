import { createPortal } from "react-dom";
import { Link } from "react-router";
import { motion } from "motion/react";

import { AssetSearch } from "@/components/assets/asset-search";
import { CanvasCover } from "@/components/home/canvas-cover";
import { EmptyMark } from "@/components/home/empty-mark";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useCanvasList } from "@/hooks/use-canvas-list";
import { TAP } from "@/lib/motion";
import { formatCanvasTime } from "@/utils/home/home";

/** 画布卡的网格：比素材网格的卡片宽，封面是 16:10 */
const GRID = "grid grid-cols-2 gap-2.5 md:grid-cols-[repeat(auto-fill,minmax(220px,1fr))]";

/**
 * 资产页「画布」范围：真实的画布列表，点卡片在新窗口打开画布，可按标题搜索。
 * @param searchSlot 页面头部右侧放搜索框的容器，搜索框用 portal 渲染进去
 */
export function CanvasView({ searchSlot }: { searchSlot: HTMLElement | null }) {
  const list = useCanvasList(60);
  const empty = !list.loading && !list.error && list.items.length === 0;

  return (
    <>
      {searchSlot &&
        createPortal(
          <AssetSearch value={list.query} onChange={list.setQuery} placeholder="搜索画布" />,
          searchSlot,
        )}
      <div className="h-4" />

      {list.loading ? (
        <div className={GRID}>
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="aspect-[16/10] rounded-xl" />
          ))}
        </div>
      ) : list.error ? (
        <EmptyMark
          title="加载失败"
          hint={
            <Button variant="outline" size="sm" onClick={list.reload}>
              重试
            </Button>
          }
        />
      ) : empty ? (
        <EmptyMark
          title="暂无相关资产"
          hint={list.query.trim() ? "换个关键词试试" : "新建的画布会出现在这里"}
        />
      ) : (
        <div className={GRID}>
          {list.items.map((canvas) => (
            <motion.div key={canvas.id} whileTap={TAP} className="min-w-0">
              <Link
                to={`/canvas/${canvas.id}`}
                target="_blank"
                rel="noopener"
                aria-label={`打开画布 ${canvas.title}`}
                className="text-cover-foreground focus-visible:ring-ring/60 relative block aspect-[16/10] overflow-hidden rounded-xl outline-none focus-visible:ring-3"
              >
                <CanvasCover id={canvas.id} coverUrl={canvas.coverUrl} />
                <span className="from-cover-scrim absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-7 pb-2 text-xs">
                  <span className="block truncate font-medium">{canvas.title}</span>
                  <span className="text-[11px] tabular-nums opacity-70">
                    {formatCanvasTime(canvas.updatedAt)}
                  </span>
                </span>
              </Link>
            </motion.div>
          ))}
        </div>
      )}
    </>
  );
}
