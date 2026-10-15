import { Link } from "react-router";
import { motion } from "motion/react";
import { ChevronRight, Loader2, Plus } from "lucide-react";

import { CanvasCover } from "@/components/home/canvas-cover";
import { Skeleton } from "@/components/ui/skeleton";
import { RECENT_CANVAS_LIMIT } from "@/constants/creation";
import { useCanvasList } from "@/hooks/use-canvas-list";
import { useCanvasTransitionStyle } from "@/hooks/use-canvas-transition";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { CanvasListItemDto } from "@/api/canvas/type";
import { formatCanvasTime } from "@/utils/home/home";

/** 卡片的公共外框：16:10、圆角 16、细描边 */
const CARD =
  "border-border relative aspect-[16/10] min-w-0 overflow-hidden rounded-2xl border text-left";

/** 点阵底，新建卡用 */
const DOT_BG =
  "bg-card bg-[radial-gradient(circle,color-mix(in_oklch,var(--foreground)_12%,transparent)_1px,transparent_1.3px)] bg-size-[14px_14px]";

/** 一张最近画布的卡片：封面、标题和最后修改时间，点一下进画布 */
function RecentCard({ canvas }: { canvas: CanvasListItemDto }) {
  /** 点「更多」时这张卡是转场的共享元素，会飞到所有画布页里同一张卡的位置 */
  const transitionStyle = useCanvasTransitionStyle(canvas.id);
  return (
    <motion.div whileTap={TAP} style={transitionStyle} className="min-w-0">
      <Link
        to={`/canvas/${canvas.id}`}
        target="_blank"
        rel="noopener"
        aria-label={`打开画布 ${canvas.title}`}
        className={cn(
          CARD,
          "group/recent text-cover-foreground focus-visible:ring-ring/60 block outline-none focus-visible:ring-3",
        )}
      >
        <div className="size-full transition-transform duration-240 ease-out group-hover/recent:scale-[1.04] motion-reduce:group-hover/recent:scale-100">
          <CanvasCover id={canvas.id} coverUrl={canvas.coverUrl} />
        </div>
        <div className="from-cover-scrim absolute inset-0 bg-linear-to-t to-transparent to-55%" />
        <span className="absolute inset-x-3 bottom-2.5 truncate text-[13.5px] font-semibold">
          {canvas.title}
          <small className="block text-[11px] font-normal tabular-nums opacity-75">
            {formatCanvasTime(canvas.updatedAt)}
          </small>
        </span>
      </Link>
    </motion.div>
  );
}

/**
 * 创作页的「画布」tab：新建画布 + 最近的 3 张画布，超过 3 张时右上角出现「更多」，跳到所有画布页。
 * 多拉一张只用来判断「还有没有更多」，不展示。
 * 点「更多」带 view transition：新建卡和这几张画布卡会飞到所有画布页里对应的位置（设计稿 docs/design/首页 第 13 节）。
 */
export function CanvasTab() {
  const list = useCanvasList(RECENT_CANVAS_LIMIT + 1);
  const newCardStyle = useCanvasTransitionStyle("new");
  const recent = list.items.slice(0, RECENT_CANVAS_LIMIT);
  const hasMore = list.items.length > RECENT_CANVAS_LIMIT;

  return (
    <div data-slot="canvas-tab">
      {/* 这一行始终占位，「更多」出现时卡片不会往下跳 */}
      <div className="mt-4 mb-2 flex h-6 items-center justify-end">
        {hasMore && (
          <Link
            to="/canvases"
            viewTransition
            className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex items-center gap-0.5 rounded-md px-1 text-[13px] transition-colors duration-120 outline-none focus-visible:ring-3"
          >
            更多
            <ChevronRight className="size-3.5" />
          </Link>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <motion.button
          type="button"
          whileTap={list.creating ? undefined : TAP}
          style={newCardStyle}
          disabled={list.creating}
          onClick={() => void list.create()}
          className={cn(
            CARD,
            DOT_BG,
            "focus-visible:ring-ring/60 flex flex-col items-center justify-center gap-2 text-[13px] font-medium outline-none focus-visible:ring-3 disabled:cursor-progress",
          )}
        >
          <span className="bg-foreground text-background grid size-8 place-items-center rounded-lg">
            {list.creating ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
          </span>
          {list.creating ? "正在创建…" : "新建画布"}
        </motion.button>

        {list.loading
          ? Array.from({ length: RECENT_CANVAS_LIMIT }, (_, i) => (
              <Skeleton key={i} className="aspect-[16/10] rounded-2xl" />
            ))
          : recent.map((canvas) => <RecentCard key={canvas.id} canvas={canvas} />)}
      </div>
    </div>
  );
}
