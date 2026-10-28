import { motion } from "motion/react";
import { Lock } from "lucide-react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { episodePips, type PipState, type WorkspaceStage } from "@/utils/drama/stages";

import { useWorkspace } from "./context";

const PIP_STYLE: Record<PipState, string> = {
  off: "bg-muted-foreground/25",
  run: "bg-primary animate-pulse motion-reduce:animate-none",
  part: "bg-primary",
  on: "bg-status-success",
};

/** 一集下面的四格小进度条：剧本 / 拆解 / 分镜 / 成片 */
function Pips({ states }: { states: readonly PipState[] }) {
  return (
    <span aria-hidden className="flex gap-[3px]">
      {states.map((state, index) => (
        <i key={index} className={cn("h-[3px] w-1.5 rounded-full", PIP_STYLE[state])} />
      ))}
    </span>
  );
}

/**
 * 左侧分集栏：每集一个编号 + 四格进度，锁定集带小锁。
 * 「当前是哪一集」在所有工序间共用；资产区多一个「全集」用来取消按集筛选。
 */
export function EpisodeRail({
  stage,
  ep,
  onPick,
}: {
  stage: WorkspaceStage;
  ep: number;
  onPick: (ep: number) => void;
}) {
  const { drama } = useWorkspace();
  const item =
    "focus-visible:ring-ring/50 relative flex w-full flex-col items-center gap-1.5 rounded-xl py-2 outline-none transition-colors focus-visible:ring-2";
  return (
    <nav
      aria-label="分集"
      className="border-border flex w-[78px] shrink-0 flex-col gap-1 overflow-y-auto border-r px-2 py-2.5 max-sm:hidden"
    >
      <div className="text-muted-foreground py-1 text-center font-mono text-[10.5px] tracking-[0.12em]">
        {stage === "assets" ? "筛选" : "分集"}
      </div>
      {stage === "assets" && (
        <RailItem active={ep === 0} className={item} label="全集" onClick={() => onPick(0)}>
          <span className="text-[13px] font-semibold">全集</span>
          <Pips states={["on"]} />
        </RailItem>
      )}
      {drama.episodes.map((episode) => (
        <RailItem
          key={episode.no}
          active={ep === episode.no}
          className={item}
          label={`第 ${episode.no} 集 · ${episode.title}`}
          onClick={() => onPick(episode.no)}
        >
          {episode.locked && (
            <Lock
              aria-hidden
              className="text-muted-foreground absolute top-1.5 right-1.5 size-2.5"
            />
          )}
          <span className="font-mono text-[15px] leading-none font-semibold tracking-tight">
            {String(episode.no).padStart(2, "0")}
          </span>
          <Pips states={episodePips(episode)} />
        </RailItem>
      ))}
    </nav>
  );
}

function RailItem({
  active,
  className,
  label,
  onClick,
  children,
}: {
  active: boolean;
  className: string;
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-current={active}
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        className,
        active
          ? "text-foreground"
          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
      )}
    >
      {active && (
        <>
          <motion.span
            layoutId="drama-rail-active"
            transition={SPRING}
            className="bg-muted absolute inset-0 rounded-xl"
          />
          <motion.span
            layoutId="drama-rail-bar"
            transition={SPRING}
            className="bg-primary absolute top-2.5 bottom-2.5 -left-2 w-[3px] rounded-r-full"
          />
        </>
      )}
      <span className="relative flex flex-col items-center gap-1.5">{children}</span>
    </button>
  );
}
