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

/** 卡片下的三步进度点：剧本 · 资产 · 视频 */
function Stepper({ progress }: { progress: [number, number, number] }) {
  return (
    <div className="mt-1.5 flex gap-1" title="剧本 · 资产 · 视频" aria-hidden>
      {progress.map((p, i) => (
        <i
          key={i}
          className={cn(
            "h-1 flex-1 rounded-full",
            p >= 1 ? "bg-status-success" : p > 0 ? "bg-status-warning" : "bg-muted",
          )}
        />
      ))}
    </div>
  );
}

/** 一张短剧卡：点击进工作台；右上角「⋯」删除（连带画布和角色卡，要确认） */
function DramaCard({
  drama,
  index,
  deleting,
  onDelete,
}: {
  drama: DramaSummary;
  index: number;
  deleting: boolean;
  onDelete: () => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const status = dramaStatusLabel(drama.status);
  return (
    <motion.div
      data-slot="drama-card"
      layout="position"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: deleting ? 0.5 : 1, y: 0 }}
      transition={{ duration: DURATION.slow, delay: Math.min(index, STAGGER_MAX) * STAGGER }}
      whileTap={TAP_CARD}
      className="group/card relative min-w-0"
    >
      <Link
        to={`/drama/${drama.id}`}
        className="focus-visible:ring-ring/60 block rounded-xl outline-none focus-visible:ring-3"
        aria-label={`打开短剧 ${drama.title}`}
      >
        <DramaPoster seed={drama.title} className="aspect-4/3 rounded-xl">
          {drama.coverUrl && (
            <img src={drama.coverUrl} alt="" className="absolute inset-0 size-full object-cover" />
          )}
          <div className="from-cover-scrim absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-3 pt-8 pb-2.5">
            <div className="text-cover-foreground truncate text-sm font-semibold">
              {drama.title}
            </div>
          </div>
        </DramaPoster>
        <div className="px-0.5 pt-2">
          <div className="text-muted-foreground flex items-center gap-1.5 text-xs">
            {status.busy && <Loader2 className="size-3 animate-spin" />}
            <span className="truncate tabular-nums">
              {drama.episodeCount > 0 ? `${drama.episodeCount} 集 · ` : ""}
              {status.text}
              {drama.lockedCount > 0 ? ` · 已拆解 ${drama.lockedCount} 集` : ""}
            </span>
            <span className="ml-auto shrink-0 tabular-nums">
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
    <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 lg:grid-cols-4">
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
