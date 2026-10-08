import { Link } from "react-router";
import { motion } from "motion/react";
import { ArrowUp, Loader2, Plus, Workflow } from "lucide-react";

import { CanvasCover } from "@/components/home/canvas-cover";
import { SoonTip } from "@/components/home/soon";
import { Skeleton } from "@/components/ui/skeleton";
import { CANVAS_TEMPLATES } from "@/constants/creation";
import { useCanvasList } from "@/hooks/use-canvas-list";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatCanvasTime } from "@/utils/home/home";
import { placeholderBackground } from "@/utils/home/placeholder";

/** 卡片的公共外框：16:10、圆角 16、细描边 */
const CARD =
  "border-border relative aspect-[16/10] min-w-0 overflow-hidden rounded-2xl border text-left";

/** 点阵底，新建卡用 */
const DOT_BG =
  "bg-card bg-[radial-gradient(circle,color-mix(in_oklch,var(--foreground)_12%,transparent)_1px,transparent_1.3px)] bg-size-[14px_14px]";

/**
 * 创作页的「画布」tab：新建画布、最近一张画布、两个模板位，下面一行「一句话新建画布」。
 * 新建和最近画布是真功能；模板和一句话新建（要 Agent 拆分镜）还没有接口，先禁用。
 */
export function CanvasTab() {
  const list = useCanvasList(1);
  const recent = list.items[0];

  return (
    <div data-slot="canvas-tab">
      <div className="mt-7 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <motion.button
          type="button"
          whileTap={list.creating ? undefined : TAP}
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

        {list.loading ? (
          <Skeleton className="aspect-[16/10] rounded-2xl" />
        ) : recent ? (
          <motion.div whileTap={TAP} className="min-w-0">
            <Link
              to={`/canvas/${recent.id}`}
              aria-label={`打开画布 ${recent.title}`}
              className={cn(
                CARD,
                "group/recent text-cover-foreground focus-visible:ring-ring/60 block outline-none focus-visible:ring-3",
              )}
            >
              <div className="size-full transition-transform duration-240 ease-out group-hover/recent:scale-[1.04] motion-reduce:group-hover/recent:scale-100">
                <CanvasCover id={recent.id} coverUrl={recent.coverUrl} />
              </div>
              <div className="from-cover-scrim absolute inset-0 bg-linear-to-t to-transparent to-55%" />
              <span className="absolute inset-x-3 bottom-2.5 truncate text-[13.5px] font-semibold">
                {recent.title}
                <small className="block text-[11px] font-normal tabular-nums opacity-75">
                  {formatCanvasTime(recent.updatedAt)}
                </small>
              </span>
            </Link>
          </motion.div>
        ) : null}

        {CANVAS_TEMPLATES.map((template) => (
          <SoonTip key={template.title} className="block cursor-not-allowed">
            <div
              style={{ background: placeholderBackground(template.hue) }}
              className={cn(CARD, "text-cover-foreground block opacity-80")}
            >
              <div className="from-cover-scrim absolute inset-0 bg-linear-to-t to-transparent to-55%" />
              <span className="bg-cover-scrim absolute top-2 left-2 inline-grid h-5.5 place-items-center rounded-md px-2 text-[11.5px]">
                模板
              </span>
              <span className="absolute inset-x-3 bottom-2.5 truncate text-[13.5px] font-semibold">
                {template.title}
              </span>
            </div>
          </SoonTip>
        ))}
      </div>

      <SoonTip className="mx-auto mt-6 block w-[min(680px,100%)] cursor-not-allowed">
        <form
          aria-label="一句话新建画布"
          className="bg-card border-border flex h-15 items-center gap-2.5 rounded-[18px] border pr-2.5 pl-4.5 opacity-60"
        >
          <Workflow className="text-muted-foreground size-4 shrink-0" />
          <input
            disabled
            maxLength={2000}
            placeholder="输入想法或剧本，新建画布并让 Agent 拆分镜 …"
            aria-label="一句话新建画布"
            className="placeholder:text-muted-foreground h-full min-w-0 flex-1 bg-transparent text-[14.5px] outline-none"
          />
          <button
            type="submit"
            disabled
            aria-label="新建画布并开始"
            className="bg-muted text-muted-foreground grid size-9 shrink-0 place-items-center rounded-full"
          >
            <ArrowUp className="size-4.5" />
          </button>
        </form>
      </SoonTip>
    </div>
  );
}
