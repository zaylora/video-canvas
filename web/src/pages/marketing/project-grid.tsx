import { useState } from "react";
import { Link } from "react-router";
import { motion } from "motion/react";
import { Loader2, MoreHorizontal, Play, ShoppingBag, Trash2 } from "lucide-react";

import type { ProjectSummary } from "@/api/marketing/type";
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
import { formatCanvasTime } from "@/utils/home/home";
import { BAR_CLASS, projectStatusLine } from "@/utils/marketing/status";

/** 卡片下的状态条：每份脚本一格 3px，最近最多 8 份；没有脚本时留一格灰 */
function Bars({ bars }: { bars: ProjectSummary["bars"] }) {
  const cells = bars.length > 0 ? bars : (["draft"] as const);
  return (
    <div className="mt-2.5 flex gap-[3px]" aria-hidden>
      {cells.map((state, index) => (
        <i
          key={index}
          className={cn(
            "h-[3px] flex-1 rounded-full",
            BAR_CLASS[state],
            state === "rendering" && "animate-pulse motion-reduce:animate-none",
          )}
        />
      ))}
    </div>
  );
}

/** 一张营销片卡：点击进详情；悬停右上角「⋯」打开 / 删除（要确认）。营销首页和作品页共用 */
export function ProjectCard({
  project,
  index,
  deleting,
  onDelete,
  instant,
}: {
  project: ProjectSummary;
  index: number;
  /** 不做入场和位移动画（作品页切换范围时直接换内容） */
  instant?: boolean;
  deleting: boolean;
  onDelete: () => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const status = projectStatusLine(project);
  return (
    <motion.div
      data-slot="marketing-card"
      layout={instant ? false : "position"}
      initial={instant ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: deleting ? 0.5 : 1, y: 0 }}
      transition={{ duration: DURATION.slow, delay: Math.min(index, STAGGER_MAX) * STAGGER }}
      whileTap={TAP_CARD}
      className="group/card relative min-w-0"
    >
      <Link
        to={`/marketing/${project.id}`}
        className="focus-visible:ring-ring/60 block rounded-[18px] outline-none focus-visible:ring-3"
        aria-label={`打开营销片 ${project.title}`}
      >
        <div className="bg-muted ring-chrome-border group-hover/card:ring-foreground/20 relative aspect-16/10 overflow-hidden rounded-[18px] ring-1 transition-shadow duration-150">
          {project.coverUrl ? (
            <img
              src={project.coverUrl}
              alt=""
              className="absolute inset-0 size-full object-cover"
            />
          ) : (
            <ShoppingBag className="text-muted-foreground absolute inset-0 m-auto size-8 opacity-40" />
          )}
          {project.scriptCounts.done > 0 && (
            <span className="absolute inset-0 grid place-items-center">
              <span className="bg-cover-scrim text-cover-foreground grid size-10 place-items-center rounded-full">
                <Play className="size-4 fill-current" />
              </span>
            </span>
          )}
        </div>
        <div className="px-1 pt-3">
          <div className="truncate text-[15px] font-medium">{project.title}</div>
          <div className="text-muted-foreground mt-1 flex items-center gap-1.5 text-[13px]">
            {status.tone === "busy" || status.tone === "running" ? (
              <Loader2
                className={cn(
                  "size-3 shrink-0 animate-spin",
                  status.tone === "running" && "text-status-running",
                )}
              />
            ) : null}
            <span
              className={cn(
                "truncate",
                status.tone === "bad" && "text-destructive",
                status.tone === "running" && "text-status-running",
              )}
            >
              {status.text}
            </span>
            <span className="ml-auto shrink-0 tabular-nums">
              {formatCanvasTime(project.updatedAt)}
            </span>
          </div>
          <Bars bars={project.bars} />
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
          <DropdownMenuItem render={<Link to={`/marketing/${project.id}`} />}>
            打开
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={() => setConfirm(true)}>
            <Trash2 />
            删除
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{project.title}」？</AlertDialogTitle>
            <AlertDialogDescription>
              脚本和轮次会删除，已出的成片保留在资产里。
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

/** 「我的营销片」网格 */
export function ProjectGrid({
  items,
  deletingId,
  onDelete,
}: {
  items: ProjectSummary[];
  deletingId: string | null;
  onDelete: (id: string) => void;
}) {
  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-7 sm:grid-cols-3 lg:grid-cols-4">
      {items.map((project, index) => (
        <ProjectCard
          key={project.id}
          project={project}
          index={index}
          deleting={deletingId === project.id}
          onDelete={() => onDelete(project.id)}
        />
      ))}
    </div>
  );
}
