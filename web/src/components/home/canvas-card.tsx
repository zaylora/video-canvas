import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { motion } from "motion/react";
import { Loader2, MoreHorizontal, Plus, Trash2 } from "lucide-react";

import type { CanvasListItemDto } from "@/api/canvas/type";
import { CanvasCover } from "@/components/home/canvas-cover";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT, STAGGER, STAGGER_MAX, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatCanvasTime } from "@/utils/home/home";

/**
 * 画布卡的两种外观（设计稿 docs/design/首页 第 5 节）：
 * overlay 是首页最近画布的小卡，标题叠在封面底部的渐变上；
 * panel 是所有画布页的大卡，封面在上、标题在卡片底部。
 */
export type CanvasCardVariant = "overlay" | "panel";

/** 卡片外框：overlay 本身就是 4:3 封面，panel 是带内边距的卡片 */
const FRAME = {
  overlay: "aspect-[4/3] rounded-xl",
  panel: "bg-card ring-border rounded-2xl p-1.5 ring-1",
} as const;

/** 列表里第 index 张卡的入场：从下方 6px 浮上来，相邻的错开一点 */
function enterMotion(index: number) {
  return {
    initial: { opacity: 0, y: 6 },
    animate: { opacity: 1, y: 0 },
    /** 退出比进入快，约为进入的 70% */
    exit: { opacity: 0, transition: { duration: DURATION.base * 0.7 } },
    transition: {
      duration: DURATION.base,
      ease: EASE_OUT,
      delay: Math.min(index, STAGGER_MAX) * STAGGER,
    },
  };
}

/** 封面悬停放大用 CSS 过渡（跟着卡片的 group-hover），时长和曲线仍取自 lib/motion */
const COVER_TRANSITION = {
  transitionDuration: `${DURATION.base}s`,
  transitionTimingFunction: `cubic-bezier(${EASE_OUT.join(",")})`,
};

/** 卡片右上角的「⋯」：目前只有删除，删除不能撤销，所以要先确认 */
function CardActions({ title, onDelete }: { title: string; onDelete: () => void }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  return (
    <>
      <DropdownMenu modal={false} open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger
          aria-label="更多操作"
          className={cn(
            "bg-cover-scrim absolute top-2 right-2 z-10 grid size-7 place-items-center rounded-full text-cover-foreground outline-none",
            "opacity-0 transition-opacity duration-150 group-hover/card:opacity-100 focus-visible:opacity-100 data-popup-open:opacity-100",
            "focus-visible:ring-2 focus-visible:ring-cover-foreground/70 max-md:opacity-100 [&_svg]:size-4",
          )}
        >
          <MoreHorizontal />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" sideOffset={6} className="w-36">
          <DropdownMenuItem variant="destructive" onClick={() => setConfirmOpen(true)}>
            <Trash2 />
            删除
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{title}」？</AlertDialogTitle>
            <AlertDialogDescription>
              画布里的节点和连线会一起删除，删除后无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirmOpen(false);
                onDelete();
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/** 一张画布卡：点击进画布（用 Link，中键能新开标签页），右上角「⋯」可删除 */
export function CanvasCard({
  canvas,
  variant,
  index,
  deleting,
  onDelete,
}: {
  canvas: CanvasListItemDto;
  variant: CanvasCardVariant;
  /** 在列表里的位置，决定入场错开多少 */
  index: number;
  /** 删除请求进行中：半透明、不可点 */
  deleting?: boolean;
  onDelete: () => void;
}) {
  const time = formatCanvasTime(canvas.updatedAt);

  return (
    <motion.div
      layout
      data-slot="canvas-card"
      data-variant={variant}
      {...enterMotion(index)}
      whileTap={TAP}
      className={cn(
        "group/card relative min-w-0",
        FRAME[variant],
        variant === "overlay" && "overflow-hidden",
        deleting && "pointer-events-none opacity-50",
      )}
    >
      <Link
        to={`/canvas/${canvas.id}`}
        aria-label={`打开画布 ${canvas.title}`}
        className={cn(
          "block outline-none",
          variant === "overlay" ? "absolute inset-0" : "rounded-xl",
          "focus-visible:ring-ring/60 focus-visible:ring-3",
          variant === "overlay" && "rounded-xl focus-visible:ring-inset",
        )}
      >
        <div
          className={cn(
            "relative overflow-hidden",
            variant === "overlay" ? "absolute inset-0" : "aspect-[4/3] rounded-xl",
          )}
        >
          <div
            style={COVER_TRANSITION}
            className="size-full transition-transform group-hover/card:scale-[1.03] motion-reduce:group-hover/card:scale-100"
          >
            <CanvasCover id={canvas.id} coverUrl={canvas.coverUrl} />
          </div>
        </div>
        {variant === "overlay" ? (
          <div className="from-cover-scrim absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-3 pt-7 pb-2.5 text-cover-foreground">
            <div className="truncate text-[13px] font-semibold">{canvas.title}</div>
            <div className="text-[11px] tabular-nums opacity-75">{time}</div>
          </div>
        ) : (
          <div className="px-2 pt-2.5 pb-1.5">
            <div className="truncate text-[15px] font-semibold">{canvas.title}</div>
            <div className="text-muted-foreground text-xs tabular-nums">{time}</div>
          </div>
        )}
      </Link>
      <div className={cn(variant === "panel" && "absolute inset-x-1.5 top-1.5")}>
        <CardActions title={canvas.title} onDelete={onDelete} />
      </div>
    </motion.div>
  );
}

/** 两种卡片底部信息区的占位高度，新建卡和骨架屏靠它和真卡对齐 */
function PanelInfoSpacer() {
  return (
    <div aria-hidden className="invisible px-2 pt-2.5 pb-1.5">
      <div className="text-[15px]">&nbsp;</div>
      <div className="text-xs">&nbsp;</div>
    </div>
  );
}

/**
 * 「＋ 新建画布」卡：和画布卡用同一套骨架（4:3 封面区 + 信息区），
 * 「＋」叠在上面居中，所以列表为空或失败时也是完整卡高。
 */
export function NewCanvasCard({
  variant,
  creating,
  onCreate,
}: {
  variant: CanvasCardVariant;
  creating: boolean;
  onCreate: () => void;
}) {
  return (
    <motion.button
      type="button"
      layout
      data-slot="new-canvas-card"
      disabled={creating}
      whileTap={creating ? undefined : TAP}
      onClick={onCreate}
      className={cn(
        "group/new text-muted-foreground relative min-w-0 text-left outline-none",
        "hover:text-foreground focus-visible:ring-ring/60 transition-colors duration-150 focus-visible:ring-3 disabled:cursor-progress",
        FRAME[variant],
      )}
    >
      {variant === "panel" && (
        <>
          <div className="aspect-[4/3]" />
          <PanelInfoSpacer />
        </>
      )}
      <span
        className={cn(
          "bg-muted/40 ring-border group-hover/new:bg-muted absolute flex flex-col items-center justify-center gap-2.5 ring-1 transition-colors duration-150 ring-inset",
          variant === "overlay" ? "inset-0 rounded-xl" : "inset-1.5 rounded-xl",
        )}
      >
        <span
          className={cn(
            "bg-chrome-hover ring-chrome-border grid place-items-center rounded-full ring-1",
            variant === "overlay" ? "size-10" : "size-12",
          )}
        >
          {creating ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-5" />}
        </span>
        <span className={variant === "overlay" ? "text-[13px]" : "text-sm"}>
          {creating ? "正在创建…" : "新建画布"}
        </span>
      </span>
    </motion.button>
  );
}

/** 加载中的卡片骨架，尺寸和真卡一致 */
export function CanvasCardSkeleton({ variant }: { variant: CanvasCardVariant }) {
  if (variant === "overlay") return <Skeleton className="aspect-[4/3] rounded-xl" />;
  return (
    <div className={FRAME.panel}>
      <Skeleton className="aspect-[4/3] rounded-xl" />
      <div className="px-2 pt-3 pb-2">
        <Skeleton className="h-3.5 w-1/2" />
        <Skeleton className="mt-2 h-3 w-1/3" />
      </div>
    </div>
  );
}

/** 卡片列表旁边的说明：空、搜索无结果、失败，在剩余列里垂直居中 */
export function CardListNote({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        "text-muted-foreground flex flex-col items-center justify-center gap-2 self-center py-6 text-center text-[13px]",
        className ?? "col-span-full sm:col-start-2",
      )}
    >
      {children}
    </div>
  );
}
