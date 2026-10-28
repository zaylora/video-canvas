import { useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import {
  Check,
  FilePenLine,
  ListTree,
  Loader2,
  Lock,
  ScanSearch,
  Sparkles,
  Users,
} from "lucide-react";
import { toast } from "sonner";

import { addEpisode, getEpisode, submitJob, updateEpisode } from "@/api/drama";
import { dramaErrorMessage, isRevisionConflict } from "@/api/drama/errors";
import type { Episode } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDramaJobsStore } from "@/store/drama";
import { episodeEditState, nextStep } from "@/utils/drama/episodes";
import { isActiveJob } from "@/utils/drama/jobs";
import { extractScenes, normalizeSceneTitle } from "@/utils/drama/scenes";

import { useWorkspace } from "./context";
import { DocEditor } from "./doc-editor";
import { useEstimate } from "./use-estimate";

/** 当前打开的文档：大纲、人物传记或第 N 集 */
type Doc = "outline" | "characters" | number;

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

/** 研读中的步骤与骨架屏 */
function AnalyzingView() {
  const { jobs } = useWorkspace();
  const outlineRunning = jobs.some((job) => job.step === "outline" && isActiveJob(job));
  const current = outlineRunning ? 1 : 0;
  const steps = [
    ["研读剧本", "判断是剧本还是小说，整理章节与人物"],
    ["故事大纲与人物传记", "提炼主线、人物关系和每集梗概"],
    ["按集拆分", "生成分集目录"],
  ] as const;
  return (
    <div className="mx-auto w-full max-w-3xl px-6 py-10">
      <h2 className="text-xl font-semibold">正在研读剧本…</h2>
      <p className="text-muted-foreground mt-1 text-sm">可以离开这个页面，完成后会通知你</p>
      <ol className="mt-6 grid gap-3">
        {steps.map(([title, detail], index) => (
          <li key={title} className="flex items-start gap-3">
            <span
              className={cn(
                "mt-0.5 grid size-5 shrink-0 place-items-center rounded-full border",
                index < current && "bg-status-success border-transparent text-white",
                index === current && "border-status-running text-status-running",
              )}
            >
              {index < current ? (
                <Check className="size-3" />
              ) : index === current ? (
                <Loader2 className="size-3 animate-spin" />
              ) : null}
            </span>
            <div>
              <div className="text-sm font-medium">{title}</div>
              <div className="text-muted-foreground text-xs">{detail}</div>
            </div>
          </li>
        ))}
      </ol>
      <div className="mt-8 grid gap-2.5">
        {[96, 88, 92, 70, 84].map((width) => (
          <Skeleton
            key={width}
            className="h-4 motion-reduce:animate-none"
            style={{ width: `${width}%` }}
          />
        ))}
      </div>
    </div>
  );
}

/**
 * 稿纸的排版：集名用衬线粗体，场次标题（二到四级标题）是等宽灰底的小标签，
 * 角色名、台词沿用编辑器自带的样式。浅色是米白纸，深色是比底色亮一档的纸。
 */
const PAPER_PROSE =
  "[&_h1]:font-display [&_h1]:font-black [&_h1]:tracking-wide " +
  "[&_h2]:bg-muted [&_h2]:inline-flex [&_h2]:rounded-md [&_h2]:px-3 [&_h2]:py-1.5 [&_h2]:font-mono [&_h2]:text-[12.5px] [&_h2]:tracking-wider " +
  "[&_h3]:bg-muted [&_h3]:inline-flex [&_h3]:rounded-md [&_h3]:px-3 [&_h3]:py-1.5 [&_h3]:font-mono [&_h3]:text-[12.5px] [&_h3]:tracking-wider";

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
          <>
            {summary.scriptStatus !== "ready" && !episode.scriptMd && (
              <div className="text-muted-foreground mt-10 flex flex-col items-center gap-1 text-center text-sm">
                <FilePenLine className="size-7" />
                <div>
                  {summary.scriptStatus === "failed" ? "这一集的剧本生成失败" : "这一集还没有剧本"}
                </div>
                <div className="text-xs">可以直接在下面手写，或用底部的「生成分集剧本」</div>
              </div>
            )}
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
          </>
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

/** 大纲 / 人物传记：随短剧一起保存（同一个 revision） */
function ProjectDoc({ doc }: { doc: "outline" | "characters" }) {
  const { drama, patch, reportSave } = useWorkspace();
  const markdown = doc === "outline" ? drama.outlineMd : drama.charactersMd;
  return (
    <div className="mx-auto w-full max-w-[720px] px-6 pt-6 pb-28 max-sm:px-3">
      <h2 className="font-display text-[22px] font-black tracking-wide">
        {doc === "outline" ? "故事大纲" : "人物传记"}
      </h2>
      <div
        className={cn(
          "bg-paper text-paper-foreground ring-border mt-4 rounded-t-md rounded-b-2xl px-[clamp(20px,6vw,72px)] py-12 shadow-[0_24px_60px_-24px_rgb(0_0_0/0.5)] ring-1",
          PAPER_PROSE,
        )}
      >
        <DocEditor
          key={doc}
          value={markdown}
          revision={drama.revision}
          editable
          isConflict={isRevisionConflict}
          onStatus={(status) => reportSave(doc, status)}
          onSave={(next) => patch(doc === "outline" ? { outlineMd: next } : { charactersMd: next })}
        />
      </div>
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
            {step.kind === "scripts" ? <Sparkles /> : <ScanSearch />}
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
 * 剧本区：左边目录（大纲 / 人物传记 / 第 N 集，带状态图标），右边文档（Markdown 编辑、自动保存），
 * 研读中显示步骤和骨架屏，底部「下一步」浮条。
 */
export function ScriptArea({
  ep,
  onEp,
  onBreakdownOpen,
}: {
  /** 当前集号（来自左侧分集栏） */
  ep: number;
  onEp: (ep: number) => void;
  onBreakdownOpen: () => void;
}) {
  const { drama, jobs, refresh } = useWorkspace();
  /** 看大纲 / 人物传记时记在这里；null 表示看分集栏选中的那一集 */
  const [project, setProject] = useState<"outline" | "characters" | null>(null);
  const doc: Doc = project ?? ep;
  const setDoc = (next: Doc) => {
    if (typeof next === "number") {
      setProject(null);
      onEp(next);
    } else setProject(next);
  };
  // 分集栏换了集，回到那一集的剧本（渲染期间对比上一次的集号，不用 effect 多渲染一轮）
  const [seenEp, setSeenEp] = useState(ep);
  if (seenEp !== ep) {
    setSeenEp(ep);
    setProject(null);
  }
  const [adding, setAdding] = useState(false);
  const analyzing =
    drama.status === "analyzing" ||
    jobs.some((job) => (job.step === "analyze" || job.step === "outline") && isActiveJob(job));
  const showAnalyzing = analyzing && !drama.outlineMd && drama.episodes.length === 0;

  const addOne = async () => {
    setAdding(true);
    try {
      const created = await addEpisode(drama.id, { title: "新的一集" });
      toast.success(`已新增第 ${created.no} 集，可以直接手写剧本`);
      // 分集栏读自详情：先拉一次，新集出现后再切过去，否则集号会被当成无效回落
      await refresh();
      setDoc(created.no);
    } catch (e) {
      toast.error(dramaErrorMessage(e, "新增失败，请稍后重试"));
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="flex h-full min-h-0">
      <nav
        aria-label="剧本目录"
        className="border-border hidden w-48 shrink-0 flex-col gap-0.5 overflow-y-auto border-r p-2 md:flex"
      >
        <TocItem active={doc === "outline"} onClick={() => setDoc("outline")}>
          <ListTree className="size-4" />
          故事大纲
        </TocItem>
        <TocItem active={doc === "characters"} onClick={() => setDoc("characters")}>
          <Users className="size-4" />
          人物传记
        </TocItem>
        {!showAnalyzing && drama.episodes.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="mt-1 justify-start"
            disabled={adding}
            onClick={() => void addOne()}
          >
            + 新增一集
          </Button>
        )}
        {showAnalyzing && (
          <div className="grid gap-2 p-2">
            {[80, 60, 70, 50].map((width) => (
              <Skeleton
                key={width}
                className="h-4 motion-reduce:animate-none"
                style={{ width: `${width}%` }}
              />
            ))}
          </div>
        )}
      </nav>

      <div className="relative min-w-0 flex-1">
        <div className="h-full overflow-y-auto">
          {drama.status === "failed" && !analyzing && (
            <div
              role="alert"
              className="bg-destructive/10 text-destructive mx-auto mt-4 w-full max-w-3xl rounded-lg px-4 py-2 text-sm"
            >
              上一步任务失败了，在右侧「进度」里可以重试，失败不扣积分。
            </div>
          )}
          {showAnalyzing ? (
            <AnalyzingView />
          ) : (
            <>
              {/* 窄屏没有左侧目录：顶部用下拉切换文档 */}
              <div className="border-border flex gap-1 overflow-x-auto border-b p-2 md:hidden">
                <TocItem active={doc === "outline"} onClick={() => setDoc("outline")}>
                  大纲
                </TocItem>
                <TocItem active={doc === "characters"} onClick={() => setDoc("characters")}>
                  人物
                </TocItem>
                {drama.episodes.map((ep) => (
                  <TocItem key={ep.no} active={doc === ep.no} onClick={() => setDoc(ep.no)}>
                    第{ep.no}集
                  </TocItem>
                ))}
              </div>
              {doc === "outline" || doc === "characters" ? (
                <ProjectDoc key={doc} doc={doc} />
              ) : (
                <EpisodeDoc key={doc} no={doc} />
              )}
            </>
          )}
        </div>
        {!showAnalyzing && <NextStepPill onBreakdown={onBreakdownOpen} />}
      </div>
    </div>
  );
}
