import { useState } from "react";
import { Link } from "react-router";
import { motion } from "motion/react";
import { Loader2, MoreHorizontal, Trash2 } from "lucide-react";

import type { DramaSummary } from "@/api/drama/type";
import { DramaPoster } from "@/components/drama/poster";
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
import { DURATION, STAGGER, STAGGER_MAX, TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { dramaProgress, dramaStatusLabel } from "@/utils/drama/options";
import { formatCanvasTime } from "@/utils/home/home";

/** 卡片下的三段进度条：剧本 · 资产 · 视频 */
function Stepper({ progress }: { progress: [number, number, number] }) {
  return (
    <div className="mt-2.5 flex gap-[3px]" title="剧本 · 资产 · 视频" aria-hidden>
      {progress.map((p, i) => (
        <i
          key={i}
          className={cn(
            "h-[3px] flex-1 rounded-full",
            p >= 1 ? "bg-status-success" : p > 0 ? "bg-status-warning" : "bg-chrome-hover",
          )}
        />
      ))}
    </div>
  );
}

/** 一张短剧卡：点击进工作台；右上角「⋯」删除（连带画布和角色卡，要确认）。短剧首页和作品页共用 */
export function DramaCard({
  drama,
  index,
  deleting,
  onDelete,
  instant,
}: {
  drama: DramaSummary;
  index: number;
  /** 不做入场和位移动画（作品页切换范围时直接换内容） */
  instant?: boolean;
  deleting: boolean;
  onDelete: () => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const status = dramaStatusLabel(drama.status);
  return (
    <motion.div
      data-slot="drama-card"
      layout={instant ? false : "position"}
      initial={instant ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: deleting ? 0.5 : 1, y: 0 }}
      transition={{ duration: DURATION.slow, delay: Math.min(index, STAGGER_MAX) * STAGGER }}
      whileTap={TAP_CARD}
      className="group/card relative min-w-0"
    >
      <Link
        to={`/drama/${drama.id}`}
        className="focus-visible:ring-ring/60 block rounded-[18px] outline-none focus-visible:ring-3"
        aria-label={`打开短剧 ${drama.title}`}
      >
        <DramaPoster
          seed={drama.title}
          className="ring-chrome-border group-hover/card:ring-foreground/20 aspect-16/10 rounded-[18px] ring-1 transition-shadow duration-150"
        >
          {drama.coverUrl && (
            <img src={drama.coverUrl} alt="" className="absolute inset-0 size-full object-cover" />
          )}
        </DramaPoster>
        <div className="px-1 pt-3">
          <div className="truncate text-[15px] font-medium">{drama.title}</div>
          <div className="text-muted-foreground mt-1 flex items-center gap-1.5 text-[13px]">
            {status.busy ? (
              <Loader2 className="text-status-running size-3 shrink-0 animate-spin" />
            ) : (
              <i
                aria-hidden
                className={cn(
                  "size-1.5 shrink-0 rounded-full",
                  drama.status === "failed"
                    ? "bg-destructive"
                    : drama.status === "ready"
                      ? "bg-status-success"
                      : "bg-status-warning",
                )}
              />
            )}
            <span className={cn("truncate", drama.status === "failed" && "text-destructive")}>
              {status.text}
              {drama.lockedCount > 0 ? ` · 已拆解 ${drama.lockedCount} 集` : ""}
            </span>
            <span className="ml-auto shrink-0 tabular-nums">
              {drama.episodeCount > 0 ? `${drama.episodeCount} 集 · ` : ""}
              {formatCanvasTime(drama.updatedAt)}
            </span>
          </div>
          <Stepper progress={dramaProgress(drama)} />
        </div>
      </Link>

      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          aria-label="更多操作"
          className="bg-cover-scrim text-cover-foreground absolute top-2 right-2 z-10 grid size-7 place-items-center rounded-full opacity-0 transition-opacity duration-150 outline-none group-hover/card:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 data-popup-open:opacity-100 max-md:opacity-100 [&_svg]:size-4"
        >
          <MoreHorizontal />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" sideOffset={6} className="w-36">
          <DropdownMenuItem variant="destructive" onClick={() => setConfirm(true)}>
            <Trash2 />
            删除
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{drama.title}」？</AlertDialogTitle>
            <AlertDialogDescription>
              剧本、资产画布和为它创建的角色卡会一起删除，删除后无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirm(false);
                onDelete();
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </motion.div>
  );
}

/** 「我的短剧」网格 */
export function DramaGrid({
  items,
  deletingId,
  onDelete,
}: {
  items: DramaSummary[];
  deletingId: string | null;
  onDelete: (id: string) => void;
}) {
  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-7 sm:grid-cols-3 lg:grid-cols-4">
      {items.map((drama, index) => (
        <DramaCard
          key={drama.id}
          drama={drama}
          index={index}
          deleting={deletingId === drama.id}
          onDelete={() => onDelete(drama.id)}
        />
      ))}
    </div>
  );
}
