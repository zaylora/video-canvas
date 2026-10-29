import { useMemo } from "react";
import { motion } from "motion/react";
import {
  ArrowRight,
  Check,
  Clapperboard,
  Loader2,
  Play,
  ScanSearch,
  ScrollText,
} from "lucide-react";

import type { EpisodeSummary } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { useDramaAssets } from "@/hooks/use-drama-assets";
import { DURATION, EASE_OUT, STAGGER, STAGGER_MAX } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDramaJobsStore } from "@/store/drama";
import { nextStep } from "@/utils/drama/episodes";
import {
  episodeMatrix,
  overallProgress,
  resumeTarget,
  type MatrixCell,
  type ScriptDoc,
  type WorkspaceStage,
} from "@/utils/drama/stages";

import { useWorkspace } from "./context";

/** 矩阵的列：标题、点击后去哪道工序（待拆解要回剧本页，那里有拆解入口） */
const COLUMNS = [
  { key: "script", label: "剧本" },
  { key: "breakdown", label: "拆解" },
  { key: "storyboard", label: "分镜" },
  { key: "segments", label: "片段" },
  { key: "final", label: "成片" },
] as const;

type ColumnKey = (typeof COLUMNS)[number]["key"];

function cellTarget(column: ColumnKey, cell: MatrixCell): WorkspaceStage {
  if (column === "script") return "script";
  if (column === "breakdown") return cell.state === "done" ? "assets" : "script";
  if (column === "final") return "final";
  return "studio";
}

const CELL_TEXT: Record<ColumnKey, Partial<Record<MatrixCell["state"], string>>> = {
  script: { todo: "—" },
  breakdown: { todo: "待拆解" },
  storyboard: { todo: "待生成" },
  segments: {},
  final: { todo: "未合成" },
};

/** 矩阵里的一格：完成 / 进行中 / 做了一部分 / 待做 / 前置没做完（不可点） */
function Cell({
  column,
  cell,
  label,
  onClick,
}: {
  column: ColumnKey;
  cell: MatrixCell;
  label: string;
  onClick: () => void;
}) {
  const base =
    "focus-visible:ring-ring/50 relative flex h-[30px] items-center justify-center gap-1 overflow-hidden rounded-[7px] font-mono text-[11.5px] font-medium outline-none transition-[transform,filter] duration-150 focus-visible:ring-2";
  if (cell.state === "none") {
    return (
      <button
        type="button"
        disabled
        aria-label={`${label}，需要先完成前一道工序`}
        className={cn(base, "border-border/80 border border-dashed opacity-60")}
      />
    );
  }
  const tone =
    cell.state === "done"
      ? "bg-status-success/15 text-status-success"
      : cell.state === "todo"
        ? "bg-muted text-muted-foreground"
        : "bg-primary/15 text-primary";
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      className={cn(base, tone, "hover:brightness-125 active:scale-95")}
    >
      {cell.state === "run" && <Loader2 className="size-3.5 animate-spin" aria-hidden />}
      {cell.state === "done" && column !== "segments" && <Check className="size-3.5" aria-hidden />}
      {cell.total !== undefined && `${cell.done}/${cell.total}`}
      {cell.total === undefined && cell.state !== "done" && cell.state !== "run"
        ? (CELL_TEXT[column][cell.state] ?? (cell.state === "part" ? "已过期" : ""))
        : null}
      {cell.state === "part" && cell.total !== undefined && (
        <span
          aria-hidden
          className="bg-primary absolute bottom-0 left-0 h-0.5"
          style={{ width: `${((cell.done ?? 0) / cell.total) * 100}%` }}
        />
      )}
    </button>
  );
}

/** 一行：一集在五道工序上的状态 */
function MatrixRow({
  episode,
  index,
  onGo,
}: {
  episode: EpisodeSummary;
  index: number;
  onGo: (stage: WorkspaceStage, ep: number) => void;
}) {
  const row = episodeMatrix(episode);
  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        duration: DURATION.base,
        ease: EASE_OUT,
        delay: Math.min(index, STAGGER_MAX) * STAGGER,
      }}
      className="grid grid-cols-[minmax(150px,1.6fr)_repeat(5,minmax(60px,1fr))] items-center gap-1 max-sm:grid-cols-[110px_repeat(5,minmax(40px,1fr))]"
    >
      <button
        type="button"
        onClick={() => onGo("script", episode.no)}
        className="hover:bg-muted/60 focus-visible:ring-ring/50 flex min-w-0 items-center gap-2.5 rounded-lg px-2 py-1.5 text-left outline-none focus-visible:ring-2"
      >
        <span className="text-muted-foreground w-5 font-mono text-xs font-semibold">
          {String(episode.no).padStart(2, "0")}
        </span>
        <span className="truncate text-[13px]">{episode.title}</span>
      </button>
      {COLUMNS.map(({ key, label }) => (
        <Cell
          key={key}
          column={key}
          cell={row[key]}
          label={`第 ${episode.no} 集 ${label}`}
          onClick={() => onGo(cellTarget(key, row[key]), episode.no)}
        />
      ))}
    </motion.div>
  );
}

/** 整体完成度圆环：conic-gradient 画弧，中间留白放百分比 */
function Ring({ percent }: { percent: number }) {
  return (
    <div
      role="img"
      aria-label={`整体完成度 ${percent}%`}
      className="relative grid size-[92px] shrink-0 place-items-center rounded-full"
      style={{
        background: `conic-gradient(var(--primary) ${percent}%, var(--muted) 0)`,
      }}
    >
      <div className="bg-card absolute inset-[8px] rounded-full" />
      <b className="relative font-mono text-[22px] font-semibold">
        {percent}
        <small className="text-muted-foreground text-xs">%</small>
      </b>
    </div>
  );
}

/** 「下一步」里的一条 */
function NextItem({
  icon,
  tone,
  title,
  hint,
  action,
  disabledReason,
  onClick,
}: {
  icon: React.ReactNode;
  tone: string;
  title: string;
  hint: string;
  action: string;
  disabledReason?: string | null;
  onClick: () => void;
}) {
  return (
    <div className="hover:bg-muted/50 flex items-center gap-3 rounded-xl p-2.5 transition-colors">
      <span className={cn("grid size-[34px] shrink-0 place-items-center rounded-[10px]", tone)}>
        {icon}
      </span>
      <p className="min-w-0 flex-1 text-[13px]">
        {title}
        <small className="text-muted-foreground block truncate text-xs">{hint}</small>
      </p>
      <Button
        variant="secondary"
        size="sm"
        disabled={!!disabledReason}
        title={disabledReason ?? undefined}
        onClick={onClick}
      >
        {action}
      </Button>
    </div>
  );
}

/** 剧情简介：取大纲第一段正文，去掉 Markdown 标记，太长截断 */
function synopsisOf(outlineMd: string): string {
  const paragraph = outlineMd
    .split(/\n{2,}/)
    .map((block) =>
      block
        .replace(/^[#>\-*\s]+/gm, "")
        .replace(/[*_`]/g, "")
        .trim(),
    )
    .find((block) => block.length > 8);
  if (!paragraph) return "";
  return paragraph.length > 110 ? `${paragraph.slice(0, 110)}…` : paragraph;
}

/**
 * 总览：海报式首屏（项目名、简介、一个主按钮「继续制作」）、16 集 × 5 道工序的进度矩阵、
 * 整体完成度与下一步。矩阵每格都能点，直接跳到对应工序与集。
 * 大纲和人物传记的全文在剧本页分集栏顶上，这里只放简介和入口。
 */
export function OverviewArea({
  onGo,
  onOpenDoc,
  onBreakdownOpen,
}: {
  onGo: (stage: WorkspaceStage, ep: number) => void;
  onOpenDoc: (doc: ScriptDoc) => void;
  onBreakdownOpen: () => void;
}) {
  const { drama, jobs } = useWorkspace();
  const settled = useDramaJobsStore((state) => state.settled[drama.id] ?? 0);
  const { assets } = useDramaAssets(drama.canvasId, settled);
  const resume = useMemo(() => resumeTarget(drama.episodes), [drama.episodes]);
  const progress = useMemo(() => overallProgress(drama.episodes), [drama.episodes]);
  const next = nextStep(drama, jobs);
  const posters = assets.filter((asset) => asset.type === "character" && asset.src).slice(0, 3);
  const toShoot = drama.episodes.filter(
    (ep) => ep.locked && !(ep.segmentCount > 0 && ep.doneSegmentCount >= ep.segmentCount),
  );
  const synopsis = synopsisOf(drama.outlineMd);
  const resumeEpisode = resume ? drama.episodes.find((ep) => ep.no === resume.ep) : null;

  return (
    <div className="mx-auto max-w-[1440px] px-[clamp(16px,3vw,40px)] pt-7 pb-12">
      <section className="bg-card border-border relative grid min-h-[300px] grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)] gap-6 overflow-hidden rounded-3xl border p-9 max-lg:grid-cols-1 max-sm:p-6">
        <div
          aria-hidden
          className="absolute inset-0 opacity-90"
          style={{
            background:
              "radial-gradient(120% 90% at 85% 0%, color-mix(in oklch, var(--primary) 32%, transparent), transparent 60%), radial-gradient(90% 80% at 0% 100%, color-mix(in oklch, var(--info) 22%, transparent), transparent 65%)",
          }}
        />
        <div className="relative z-10 flex flex-col justify-center gap-3.5">
          <div className="text-primary flex items-center gap-2 font-mono text-[11px] tracking-[0.18em] uppercase before:h-px before:w-[18px] before:bg-current">
            {drama.ratio || "比例未定"} · {drama.episodeSeconds} 秒 / 集
          </div>
          <h1 className="font-display text-[clamp(34px,4.4vw,56px)] leading-[1.05] font-black tracking-wide">
            {drama.title}
          </h1>
          {synopsis && (
            <p className="text-muted-foreground max-w-[48ch] text-[15px]">
              {synopsis}
              <button
                type="button"
                onClick={() => onOpenDoc("outline")}
                className="text-foreground hover:text-primary focus-visible:ring-ring/50 ml-1.5 inline-flex items-center gap-0.5 rounded text-[13px] font-medium whitespace-nowrap outline-none focus-visible:ring-2"
              >
                读完整大纲
                <ArrowRight className="size-3.5" aria-hidden />
              </button>
            </p>
          )}
          <div className="flex flex-wrap gap-1.5">
            <span className="bg-muted text-muted-foreground rounded-full px-2.5 py-1 text-xs">
              {drama.episodes.length} 集
            </span>
            <span className="bg-muted text-muted-foreground rounded-full px-2.5 py-1 text-xs">
              {assets.length} 个资产
            </span>
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-3">
            {resume ? (
              <Button
                size="lg"
                className="h-11 px-5 text-[15px]"
                onClick={() => onGo(resume.stage, resume.ep)}
              >
                <Play className="fill-current" />
                继续制作第 {resume.ep} 集
              </Button>
            ) : (
              drama.episodes.length > 0 && (
                <Button
                  size="lg"
                  className="h-11 px-5 text-[15px]"
                  onClick={() => onGo("final", 1)}
                >
                  <Play className="fill-current" />
                  全部完成，查看成片
                </Button>
              )
            )}
            {resume && resumeEpisode && (
              <span className="text-muted-foreground text-[12.5px]">
                {resume.stage === "script"
                  ? resumeEpisode.scriptStatus === "ready"
                    ? "剧本 · 还没有拆解资产"
                    : "剧本 · 还没有写好"
                  : resume.stage === "studio"
                    ? "片场 · 还有片段没有视频"
                    : "成片 · 还没合成"}
              </span>
            )}
          </div>
        </div>
        {/* 海报：角色形象图，没有形象图时不占位 */}
        <div aria-hidden className="relative z-10 min-h-[240px] max-lg:hidden">
          {posters.map((asset, index) => (
            <motion.div
              key={asset.id}
              initial={{ opacity: 0, y: 16 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: DURATION.slow, ease: EASE_OUT, delay: index * 0.06 }}
              className="absolute top-1/2 left-1/2 aspect-[3/4] overflow-hidden rounded-[14px] shadow-2xl ring-1 ring-white/10 transition-transform duration-500 hover:scale-105"
              style={{
                width: index === 1 ? 170 : 150,
                zIndex: index === 1 ? 2 : 1,
                transform: `translate(${["-125%", "-50%", "25%"][index]}, ${["-48%", "-56%", "-46%"][index]}) rotate(${[-9, 0, 8][index]}deg)`,
              }}
            >
              <img src={asset.src ?? ""} alt="" className="size-full object-cover" />
              <span className="font-display absolute bottom-2 left-2.5 text-[13px] font-semibold text-white [text-shadow:0_1px_8px_rgb(0_0_0/0.6)]">
                {asset.name}
              </span>
            </motion.div>
          ))}
        </div>
      </section>

      <div className="mt-5 grid grid-cols-[minmax(0,1fr)_340px] items-start gap-5 max-lg:grid-cols-1">
        <section className="bg-card border-border rounded-2xl border pb-3">
          <div className="flex items-center gap-2.5 px-[18px] pt-4 pb-2.5">
            <h2 className="text-[15px] font-semibold">制作进度</h2>
            <span className="text-muted-foreground text-xs max-sm:hidden">
              点任一格跳到对应工序
            </span>
          </div>
          <div className="px-2.5" role="table" aria-label="分集与工序进度">
            <div className="text-muted-foreground mb-1 grid grid-cols-[minmax(150px,1.6fr)_repeat(5,minmax(60px,1fr))] gap-1 px-2 py-1.5 font-mono text-[11px] tracking-wider max-sm:grid-cols-[110px_repeat(5,minmax(40px,1fr))]">
              <div>分集</div>
              {COLUMNS.map(({ key, label }) => (
                <div key={key} className="text-center">
                  {label}
                </div>
              ))}
            </div>
            <div className="grid gap-1">
              {drama.episodes.map((episode, index) => (
                <MatrixRow key={episode.no} episode={episode} index={index} onGo={onGo} />
              ))}
              {drama.episodes.length === 0 && (
                <p className="text-muted-foreground px-2 py-10 text-center text-sm">
                  还没有分集，研读完成后会出现在这里
                </p>
              )}
            </div>
          </div>
        </section>

        <div className="grid gap-5">
          <section className="bg-card border-border rounded-2xl border">
            <h2 className="px-[18px] pt-4 pb-2.5 text-[15px] font-semibold">整体完成度</h2>
            <div className="flex items-center gap-[18px] px-[18px] pb-[18px]">
              <Ring percent={progress.percent} />
              <dl className="grid grid-cols-[auto_auto] gap-x-3.5 gap-y-1 text-[12.5px]">
                <dt className="text-muted-foreground">剧本</dt>
                <dd className="text-right font-mono">
                  {progress.scripts}/{drama.episodes.length}
                </dd>
                <dt className="text-muted-foreground">已拆解</dt>
                <dd className="text-right font-mono">
                  {progress.broken}/{drama.episodes.length}
                </dd>
                <dt className="text-muted-foreground">成片</dt>
                <dd className="text-right font-mono">
                  {progress.finals}/{drama.episodes.length}
                </dd>
              </dl>
            </div>
          </section>

          <section className="bg-card border-border rounded-2xl border pb-2">
            <h2 className="px-[18px] pt-4 pb-2 text-[15px] font-semibold">下一步</h2>
            <div className="grid px-2">
              {next.kind === "scripts" && (
                <NextItem
                  icon={<ScrollText className="size-4" />}
                  tone="bg-primary/15 text-primary"
                  title={`${next.episodes.length} 集还没有剧本`}
                  hint={`第 ${next.episodes.join("、")} 集`}
                  action="去剧本"
                  onClick={() => onGo("script", next.episodes[0])}
                />
              )}
              {next.kind === "breakdown" && (
                <NextItem
                  icon={<ScanSearch className="size-4" />}
                  tone="bg-info/15 text-info"
                  title={`${next.episodes.length} 集剧本已就绪`}
                  hint="拆解资产后才能生成分镜；拆解后剧本会锁定"
                  action="拆解"
                  disabledReason={next.disabledReason}
                  onClick={onBreakdownOpen}
                />
              )}
              {toShoot.length > 0 && (
                <NextItem
                  icon={<Clapperboard className="size-4" />}
                  tone="bg-status-success/15 text-status-success"
                  title={`${toShoot.length} 集等着出片`}
                  hint={`第 ${toShoot.map((ep) => ep.no).join("、")} 集`}
                  action="进片场"
                  onClick={() => onGo("studio", toShoot[0].no)}
                />
              )}
              {next.kind === "none" && toShoot.length === 0 && (
                <p className="text-muted-foreground flex items-center gap-1.5 px-2.5 py-4 text-[13px]">
                  <ArrowRight className="size-3.5" aria-hidden />
                  暂时没有待办
                </p>
              )}
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
