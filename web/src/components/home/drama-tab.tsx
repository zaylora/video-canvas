import { Link } from "react-router";
import { motion } from "motion/react";
import { ChevronRight, Plus } from "lucide-react";

import type { DramaSummary } from "@/api/drama/type";
import { DramaPoster } from "@/components/drama/poster";
import { Skeleton } from "@/components/ui/skeleton";
import { useDramaList } from "@/hooks/use-drama-list";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatCanvasTime } from "@/utils/home/home";

/** 最多展示几部最近的短剧（和「画布」tab 的最近画布一样多） */
const RECENT_LIMIT = 3;

/** 卡片的公共外框：16:10、圆角 16、细描边，和「画布」tab 一致 */
const CARD =
  "border-border relative aspect-[16/10] min-w-0 overflow-hidden rounded-2xl border text-left";

/** 点阵底，新建卡用 */
const DOT_BG =
  "bg-card bg-[radial-gradient(circle,color-mix(in_oklch,var(--foreground)_12%,transparent)_1px,transparent_1.3px)] bg-size-[14px_14px]";

/** 一部最近短剧的卡片：封面（没有封面时用渐变海报）、片名和更新时间，点一下进工作台 */
function RecentDrama({ drama }: { drama: DramaSummary }) {
  return (
    <motion.div whileTap={TAP} className="min-w-0">
      <Link
        to={`/drama/${drama.id}`}
        aria-label={`打开短剧 ${drama.title}`}
        className={cn(
          CARD,
          "group/recent text-cover-foreground focus-visible:ring-ring/60 block outline-none focus-visible:ring-3",
        )}
      >
        <div className="size-full transition-transform duration-240 ease-out group-hover/recent:scale-[1.04] motion-reduce:group-hover/recent:scale-100">
          <DramaPoster seed={drama.title} className="size-full">
            {drama.coverUrl && (
              <img
                src={drama.coverUrl}
                alt=""
                className="absolute inset-0 size-full object-cover"
              />
            )}
          </DramaPoster>
        </div>
        <div className="from-cover-scrim absolute inset-0 bg-linear-to-t to-transparent to-55%" />
        <span className="absolute inset-x-3 bottom-2.5 truncate text-[13.5px] font-semibold">
          {drama.title}
          <small className="block text-[11px] font-normal tabular-nums opacity-75">
            {formatCanvasTime(drama.updatedAt)}
          </small>
        </span>
      </Link>
    </motion.div>
  );
}

/**
 * 创作页的「短剧」tab：新建短剧（去短剧工作室首页）+ 最近的 3 部短剧，
 * 超过 3 部时右上角出现「更多」，进作品页的「个人短剧」。多拉一部只用来判断「还有没有更多」。
 */
export function DramaTab() {
  const list = useDramaList(RECENT_LIMIT + 1);
  const recent = list.items.slice(0, RECENT_LIMIT);

  return (
    <div data-slot="drama-tab">
      {/* 这一行始终占位，「更多」出现时卡片不会往下跳 */}
      <div className="mt-4 mb-2 flex h-6 items-center justify-end">
        {list.total > RECENT_LIMIT && (
          <Link
            to="/canvases?scope=drama"
            className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex items-center gap-0.5 rounded-md px-1 text-[13px] transition-colors duration-120 outline-none focus-visible:ring-3"
          >
            更多
            <ChevronRight className="size-3.5" />
          </Link>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <motion.div whileTap={TAP} className="min-w-0">
          <Link
            to="/drama"
            className={cn(
              CARD,
              DOT_BG,
              "focus-visible:ring-ring/60 flex flex-col items-center justify-center gap-2 text-[13px] font-medium outline-none focus-visible:ring-3",
            )}
          >
            <span className="bg-foreground text-background grid size-8 place-items-center rounded-lg">
              <Plus className="size-4" />
            </span>
            新建短剧
          </Link>
        </motion.div>

        {list.loading && list.items.length === 0
          ? Array.from({ length: RECENT_LIMIT }, (_, i) => (
              <Skeleton key={i} className="aspect-[16/10] rounded-2xl" />
            ))
          : recent.map((drama) => <RecentDrama key={drama.id} drama={drama} />)}
      </div>
    </div>
  );
}
