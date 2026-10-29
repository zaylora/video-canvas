import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { MotionConfig } from "motion/react";

import { Logo } from "@/components/brand/logo";
import { FocusLoader } from "@/components/focus-loader";
import { Button } from "@/components/ui/button";
import { TooltipProvider } from "@/components/ui/tooltip";
import { usePageTitle } from "@/hooks/use-page-title";
import { jobStage } from "@/utils/drama/jobs";
import { stageHints, STAGES } from "@/utils/drama/stages";
import { formatPageTitle } from "@/utils/page-title";

import { useComposeNotices } from "../use-compose";
import { AssetsArea } from "./assets-area";
import { BreakdownDialog } from "./breakdown-dialog";
import { CommandPalette } from "./command-palette";
import { DramaWorkspaceContext } from "./context";
import { EpisodeRail } from "./episode-rail";
import { FinalArea } from "./final-area";
import { JobBar } from "./job-bar";
import { OverviewArea } from "./overview-area";
import { ScriptArea } from "./script-area";
import { StudioArea } from "./studio-area";
import { Topbar } from "./topbar";
import { useWorkspaceNav } from "./use-nav";
import { useDramaWorkspace, type DramaWorkspace } from "./use-workspace";

/** 加载层开始退场时不用做什么：工作台自己的入场动画随挂载播放 */
const noop = () => {};

/**
 * 短剧工作台（/drama/:id，全屏，与 /canvas/:id 同层级）：
 * 顶栏（项目名、保存状态、工序标签、画风、比例、Agent 模型）、左侧分集栏、内容区、顶栏下方的任务进度条（短剧助手这一版不上，右栏已去掉）。
 * 当前工序、集号和剧本页打开的大纲 / 人物传记写在地址栏（?area=script&ep=3&doc=outline），刷新能回到原处。
 */
export default function DramaWorkspacePage() {
  const { id = "" } = useParams();
  // 换一部短剧整页重来，不让上一部的状态残留
  return <Workspace key={id} id={id} />;
}

function Workspace({ id }: { id: string }) {
  const workspace = useDramaWorkspace(id);
  const { drama, error, jobs, save, refresh, patch, applyEpisode, reportSave } = workspace;
  const nav = useWorkspaceNav(drama);
  const [breakdownOpen, setBreakdownOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  /** 进场加载层已收起；和进画布同一套（FocusLoader），数据到了且 Logo 聚焦完才退场 */
  const [revealed, setRevealed] = useState(false);
  const onLoaderDone = useCallback(() => setRevealed(true), []);

  usePageTitle(formatPageTitle(drama?.title, "短剧工作室"));
  useComposeNotices(id);

  const value = useMemo<DramaWorkspace | null>(
    () => (drama ? { drama, jobs, save, refresh, patch, applyEpisode, reportSave } : null),
    [drama, jobs, save, refresh, patch, applyEpisode, reportSave],
  );

  const hints = useMemo(() => stageHints(drama?.episodes ?? [], jobs), [drama?.episodes, jobs]);

  // 快捷键：⌘K 命令面板；1–5 切工序，[ ] 切集。在输入框 / 编辑器里、或开着弹窗时不响应
  const { go, setEp, ep: currentEp } = nav;
  const episodeNos = useMemo(() => drama?.episodes.map((episode) => episode.no), [drama?.episodes]);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPaletteOpen((open) => !open);
        return;
      }
      const target = event.target as HTMLElement | null;
      const typing = target?.closest("input, textarea, select, [contenteditable='true']");
      if (typing || event.metaKey || event.ctrlKey || event.altKey) return;
      if (paletteOpen || breakdownOpen || document.querySelector("[role='dialog']")) return;
      if (/^[1-5]$/.test(event.key)) {
        go(STAGES[Number(event.key) - 1].key);
      } else if ((event.key === "[" || event.key === "]") && episodeNos?.length) {
        const index = Math.max(0, episodeNos.indexOf(currentEp));
        const next = episodeNos[index + (event.key === "]" ? 1 : -1)];
        if (next !== undefined) setEp(next);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [go, setEp, currentEp, episodeNos, paletteOpen, breakdownOpen]);

  if (error && !drama) {
    return (
      <div className="drama-theme bg-background text-foreground grid h-svh place-items-center">
        <div className="flex flex-col items-center gap-3 text-sm">
          <p>{error === "notfound" ? "这部短剧不存在或已被删除" : "短剧加载失败"}</p>
          <div className="flex gap-2">
            {error === "failed" && (
              <Button variant="outline" onClick={() => void refresh()}>
                重试
              </Button>
            )}
            <Button render={<Link to="/drama" />}>返回短剧列表</Button>
          </div>
        </div>
      </div>
    );
  }
  const loader = !revealed && (
    <FocusLoader
      icon={<Logo size={56} animated />}
      plainIcon
      title="连镜"
      subtitle={drama ? `正在打开「${drama.title}」` : "正在打开短剧"}
      label={drama ? `正在打开短剧「${drama.title}」` : "正在打开短剧"}
      ready={!!value}
      onOpen={noop}
      onDone={onLoaderDone}
    />
  );
  if (!value) return loader || null;

  const { stage, ep } = nav;
  return (
    <>
      <DramaWorkspaceContext.Provider value={value}>
        <MotionConfig reducedMotion="user">
          <TooltipProvider delay={400}>
            <div className="drama-theme bg-background text-foreground flex h-svh w-svw flex-col overflow-hidden">
              <Topbar
                stage={stage}
                hints={hints}
                onStage={(next) => nav.go(next)}
                onPalette={() => setPaletteOpen(true)}
              />
              <JobBar
                inPlace={{
                  analyzingView:
                    stage === "script" &&
                    nav.doc !== null &&
                    !value.drama.outlineMd &&
                    value.drama.episodes.length === 0,
                  storyboardEp: stage === "studio" ? ep : null,
                }}
              />
              <div className="flex min-h-0 flex-1">
                {stage !== "overview" && (
                  <EpisodeRail
                    stage={stage}
                    ep={ep}
                    doc={nav.doc}
                    onPick={nav.setEp}
                    onDoc={nav.openDoc}
                  />
                )}
                <main className="relative min-w-0 flex-1 overflow-y-auto">
                  {stage === "overview" && (
                    <OverviewArea
                      onGo={nav.go}
                      onOpenDoc={nav.openDoc}
                      onBreakdownOpen={() => setBreakdownOpen(true)}
                    />
                  )}
                  {stage === "script" && (
                    <ScriptArea
                      ep={ep}
                      doc={nav.doc}
                      onEp={nav.setEp}
                      onDoc={nav.openDoc}
                      onBreakdownOpen={() => setBreakdownOpen(true)}
                    />
                  )}
                  {stage === "assets" && (
                    <AssetsArea ep={ep} onBreakdownOpen={() => setBreakdownOpen(true)} />
                  )}
                  {stage === "studio" && <StudioArea ep={ep} onGo={nav.go} />}
                  {stage === "final" && <FinalArea ep={ep} onGo={nav.go} />}
                </main>
              </div>
            </div>
            <BreakdownDialog
              open={breakdownOpen}
              onOpenChange={setBreakdownOpen}
              onStarted={() => nav.go(jobStage("breakdown"), 0)}
            />
            <CommandPalette
              open={paletteOpen}
              onOpenChange={setPaletteOpen}
              onGo={nav.go}
              onBreakdownOpen={() => setBreakdownOpen(true)}
            />
          </TooltipProvider>
        </MotionConfig>
      </DramaWorkspaceContext.Provider>
      {loader}
    </>
  );
}
