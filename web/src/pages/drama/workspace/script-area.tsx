import { useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Loader2, Lock } from "lucide-react";
import { toast } from "sonner";

import { getEpisode, submitJob, updateEpisode } from "@/api/drama";
import { dramaErrorMessage, isRevisionConflict } from "@/api/drama/errors";
import type { Episode } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDramaJobsStore } from "@/store/drama";
import { episodeEditState, nextStep } from "@/utils/drama/episodes";
import { extractScenes, normalizeSceneTitle } from "@/utils/drama/scenes";
import type { ScriptDoc } from "@/utils/drama/stages";

import { useWorkspace } from "./context";
import { DocEditor } from "./doc-editor";
import { useAnalyzing } from "./analyzing-view";
import { PAPER_PROSE, ProjectDoc } from "./project-doc";
import { useEstimate } from "./use-estimate";

/** 目录里的一项 */
function TocItem({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-current={active}
      onClick={onClick}
      className={cn(
        "focus-visible:ring-ring/50 flex h-8 w-full items-center gap-2 rounded-lg px-2 text-left text-sm outline-none focus-visible:ring-2",
        active
          ? "bg-muted font-medium"
          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

/** 一集的剧本：读单集（含正文）、锁定只读、自动保存 */
function EpisodeDoc({ no }: { no: number }) {
  const { drama, applyEpisode, reportSave } = useWorkspace();
  const summary = drama.episodes.find((ep) => ep.no === no);
  const [episode, setEpisode] = useState<Episode | null>(null);
  const [failed, setFailed] = useState(false);
  const revision = summary?.revision;
  const scriptStatus = summary?.scriptStatus;
  /** 已经读到（或自己刚存下）的 revision + 状态：目录里的值和它一致就不用再读，免得每次自动保存都多一次请求 */
  const known = useRef<string | null>(null);
  const paper = useRef<HTMLDivElement>(null);
  const scriptMd = episode?.scriptMd ?? "";
  const scenes = useMemo(() => extractScenes(scriptMd), [scriptMd]);

  /** 点场次目录：按标题在稿纸里找到对应节点，滚过去（减弱动态时直接跳） */
  const jumpTo = (title: string) => {
    const nodes = paper.current?.querySelectorAll<HTMLElement>("h2, h3, h4, strong");
    const hit = [...(nodes ?? [])].find(
      (node) => normalizeSceneTitle(node.textContent ?? "") === title,
    );
    if (!hit) return;
    const target = hit.tagName === "STRONG" ? (hit.parentElement ?? hit) : hit;
    target.style.scrollMarginTop = "24px";
    target.scrollIntoView({
      block: "start",
      behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
    });
  };

  // 目录里这一集的 revision / 状态变了（生成完、任务改了）：重新读正文
  useEffect(() => {
    const key = `${revision}:${scriptStatus}`;
    if (known.current === key) return;
    let active = true;
    getEpisode(drama.id, no)
      .then((value) => {
        if (!active) return;
        known.current = `${value.revision}:${value.scriptStatus}`;
        setEpisode(value);
        setFailed(false);
      })
      .catch(() => active && setFailed(true));
    return () => {
      active = false;
    };
  }, [drama.id, no, revision, scriptStatus]);

  if (!summary) return null;
  const edit = episodeEditState(summary);
  if (failed && !episode) {
    return (
      <div className="text-muted-foreground p-10 text-center text-sm">
        这一集读取失败，请稍后重试
      </div>
    );
  }
  if (!episode) {
    return (
      <div className="mx-auto grid w-full max-w-3xl gap-2.5 px-6 py-10">
        {[90, 70, 80].map((width) => (
          <Skeleton
            key={width}
            className="h-4 motion-reduce:animate-none"
            style={{ width: `${width}%` }}
          />
        ))}
      </div>
    );
  }

  return (
    <div className="flex justify-center gap-8 px-6 pt-6 pb-28 max-sm:px-3">
      <div className="w-full max-w-[720px] min-w-0">
        <div className="flex items-center gap-2">
          <h2 className="font-display min-w-0 truncate text-[22px] font-black tracking-wide">
            第 {no} 集 · {summary.title}
          </h2>
          {summary.locked && (
            <span className="bg-status-warning/15 text-status-warning inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs">
              <Lock className="size-3" />
              已锁定
            </span>
          )}
        </div>
        {summary.locked && (
          <div className="bg-status-warning/10 mt-3 flex items-center gap-2 rounded-lg px-3 py-2 text-sm">
            <Lock className="text-status-warning size-4 shrink-0" />
            这一集已经拆解资产，剧本已锁定。资产和分镜都以这一版为准。
          </div>
        )}
        {summary.scriptStatus === "generating" ? (
          <div className="text-muted-foreground mt-16 flex flex-col items-center gap-2 text-center text-sm">
            <Loader2 className="size-6 animate-spin" />
            正在生成这一集的剧本…
          </div>
        ) : (
          <div
            ref={paper}
            className={cn(
              "bg-paper text-paper-foreground ring-border relative mt-4 rounded-t-md rounded-b-2xl px-[clamp(20px,6vw,72px)] py-12 shadow-[0_24px_60px_-24px_rgb(0_0_0/0.5)] ring-1 transition-[filter] duration-200",
              "before:absolute before:inset-y-0 before:left-9 before:w-px before:bg-[repeating-linear-gradient(180deg,var(--border)_0_3px,transparent_3px_9px)] max-sm:before:hidden",
              summary.locked && "saturate-[.7]",
              PAPER_PROSE,
            )}
          >
            <DocEditor
              key={`ep${no}`}
              value={episode.scriptMd}
              revision={episode.revision}
              editable={edit.editable}
              isConflict={isRevisionConflict}
              onStatus={(status) => reportSave(`ep${no}`, status)}
              onSave={async (markdown) => {
                try {
                  const saved = await updateEpisode(drama.id, no, {
                    revision: episode.revision,
                    scriptMd: markdown,
                  });
                  known.current = `${saved.revision}:${saved.scriptStatus}`;
                  setEpisode(saved);
                  applyEpisode(no, {
                    revision: saved.revision,
                    scriptStatus: saved.scriptStatus,
                    updatedAt: saved.updatedAt,
                  });
                  return saved.revision;
                } catch (e) {
                  if (isRevisionConflict(e)) {
                    toast.warning(`第 ${no} 集在别处被修改过，已加载最新内容，你的改动没有保存`);
                    const latest = await getEpisode(drama.id, no).catch(() => null);
                    if (latest) {
                      setEpisode(latest);
                      applyEpisode(no, { revision: latest.revision });
                    }
                  } else {
                    toast.error(dramaErrorMessage(e, "保存失败，请稍后重试"));
                  }
                  throw e;
                }
              }}
            />
          </div>
        )}
      </div>
      {/* 页边：场次目录和梗概，宽屏才有 */}
      <aside className="sticky top-6 hidden w-52 shrink-0 self-start pt-12 xl:block">
        <div className="text-muted-foreground mb-2 font-mono text-[10.5px] tracking-[0.14em]">
          本集场次
        </div>
        {scenes.length > 0 ? (
          <ol className="grid gap-0.5">
            {scenes.map((title, index) => (
              <li key={`${index}:${title}`}>
                <button
                  type="button"
                  onClick={() => jumpTo(title)}
                  className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 w-full truncate rounded-lg px-2 py-1.5 text-left text-[13px] outline-none focus-visible:ring-2"
                >
                  {title}
                </button>
              </li>
            ))}
          </ol>
        ) : (
          <p className="text-muted-foreground text-xs">
            剧本里没有识别到场次标题（二到四级标题，或加粗的「1-1 地点」）
          </p>
        )}
        {summary.synopsis && (
          <div className="border-border mt-5 border-l-2 pl-3">
            <b className="text-muted-foreground block text-[12.5px] font-medium">梗概</b>
            <p className="text-muted-foreground mt-0.5 text-xs leading-5">{summary.synopsis}</p>
          </div>
        )}
      </aside>
    </div>
  );
}

/** 底部「下一步」浮条：生成分集剧本 / 资产拆解；有进行中任务时禁用并写明原因 */
function NextStepPill({ onBreakdown }: { onBreakdown: () => void }) {
  const { drama, jobs, refresh } = useWorkspace();
  const step = nextStep(drama, jobs);
  const estimate = useEstimate(
    drama.id,
    step.kind === "scripts" ? { step: "scripts", episodes: step.episodes } : null,
  );
  const [submitting, setSubmitting] = useState(false);

  const generate = async () => {
    if (step.kind !== "scripts") return;
    setSubmitting(true);
    try {
      const job = await submitJob(drama.id, { step: "scripts", episodes: step.episodes });
      useDramaJobsStore.getState().applyJob(drama.id, job);
      void refresh();
    } catch (e) {
      toast.error(dramaErrorMessage(e, "提交失败，请稍后重试"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AnimatePresence>
      {step.kind !== "none" && (
        <motion.div
          key={step.kind}
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 8, transition: { duration: DURATION.exit } }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          className="bg-chrome ring-chrome-border absolute bottom-4 left-1/2 z-10 flex max-w-[calc(100%-2rem)] -translate-x-1/2 items-center gap-3 rounded-full py-1.5 pr-1.5 pl-4 text-sm shadow-lg ring-1 backdrop-blur-xl"
        >
          {step.kind === "scripts" ? (
            <>
              <span className="tabular-nums">{step.episodes.length} 集还没有剧本</span>
              {estimate.status === "ready" && (
                <span className="text-muted-foreground text-xs tabular-nums">
                  约 <span className="text-credit">{estimate.credits}</span> 积分
                </span>
              )}
            </>
          ) : (
            <span>剧本整理完毕，下一步拆解角色和场景</span>
          )}
          <Button
            size="sm"
            className="rounded-full"
            disabled={!!step.disabledReason || submitting}
            title={step.disabledReason ?? undefined}
            onClick={step.kind === "scripts" ? () => void generate() : onBreakdown}
          >
            {step.kind === "scripts" ? "生成分集剧本" : "资产拆解"}
          </Button>
          {step.disabledReason && (
            <span className="text-muted-foreground max-w-48 truncate pr-3 text-xs">
              {step.disabledReason}
            </span>
          )}
        </motion.div>
      )}
    </AnimatePresence>
  );
}

/**
 * 剧本区：文档（Markdown 编辑、自动保存），换集、打开大纲 / 人物传记都用左侧分集栏。
 * 研读中大纲页里显示步骤和骨架屏，底部「下一步」浮条。
 */
export function ScriptArea({
  ep,
  doc,
  onEp,
  onDoc,
  onBreakdownOpen,
}: {
  /** 当前集号（来自左侧分集栏） */
  ep: number;
  /** 打开的大纲 / 人物传记；null 看当前集的剧本 */
  doc: ScriptDoc | null;
  onEp: (ep: number) => void;
  onDoc: (doc: ScriptDoc) => void;
  onBreakdownOpen: () => void;
}) {
  const { drama } = useWorkspace();
  const { analyzing, showAnalyzing } = useAnalyzing();

  return (
    <div className="flex h-full min-h-0">
      <div className="relative min-w-0 flex-1">
        <div className="h-full overflow-y-auto">
          {drama.status === "failed" && !analyzing && (
            <div
              role="alert"
              className="bg-destructive/10 text-destructive mx-auto mt-4 w-full max-w-3xl rounded-lg px-4 py-2 text-sm"
            >
              上一步任务失败了，可以在上方重试，失败不扣积分。
            </div>
          )}
          {/* 窄屏没有左侧分集栏：顶部用标签切换大纲、人物和集 */}
          <div className="border-border flex gap-1 overflow-x-auto border-b p-2 sm:hidden">
            <TocItem active={doc === "outline"} onClick={() => onDoc("outline")}>
              大纲
            </TocItem>
            <TocItem active={doc === "characters"} onClick={() => onDoc("characters")}>
              人物
            </TocItem>
            {drama.episodes.map((item) => (
              <TocItem key={item.no} active={!doc && ep === item.no} onClick={() => onEp(item.no)}>
                第{item.no}集
              </TocItem>
            ))}
          </div>
          {doc ? <ProjectDoc key={doc} doc={doc} /> : <EpisodeDoc key={ep} no={ep} />}
        </div>
        {!showAnalyzing && <NextStepPill onBreakdown={onBreakdownOpen} />}
      </div>
    </div>
  );
}
