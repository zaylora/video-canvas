import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { MotionConfig } from "motion/react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { usePageTitle } from "@/hooks/use-page-title";
import { jobStage } from "@/utils/drama/jobs";
import { stageHints, STAGES } from "@/utils/drama/stages";
import { formatPageTitle } from "@/utils/page-title";

import { DramaAssistantProvider } from "../assistant/use-drama-assistant";
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
import { SidePanel } from "./side-panel";
import { StudioArea } from "./studio-area";
import { Topbar } from "./topbar";
import { useWorkspaceNav } from "./use-nav";
import { useDramaWorkspace, type DramaWorkspace } from "./use-workspace";

function WorkspaceSkeleton() {
  return (
    <div className="flex h-svh w-svw">
      <Skeleton className="h-full w-[78px] rounded-none motion-reduce:animate-none" />
      <div className="flex-1 p-6">
        <Skeleton className="h-8 w-60 motion-reduce:animate-none" />
        <Skeleton className="mt-6 h-64 w-full motion-reduce:animate-none" />
      </div>
    </div>
  );
}

/**
 * 短剧工作台（/drama/:id，全屏，与 /canvas/:id 同层级）：
 * 顶栏（项目名、保存状态、工序标签、画风、比例、Agent 模型）、左侧分集栏、内容区、右栏（助手）、顶栏下方的任务进度条。
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
  const [sideOpen, setSideOpen] = useState(() => window.innerWidth >= 1280);
  const [breakdownOpen, setBreakdownOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);

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
  if (!value) {
    return (
      <div className="drama-theme bg-background">
        <WorkspaceSkeleton />
      </div>
    );
  }

  const { stage, ep } = nav;
  return (
    <DramaWorkspaceContext.Provider value={value}>
      <DramaAssistantProvider drama={value.drama} refresh={refresh}>
        <MotionConfig reducedMotion="user">
          <TooltipProvider delay={400}>
            <div className="drama-theme bg-background text-foreground flex h-svh w-svw flex-col overflow-hidden">
              <Topbar
                sideOpen={sideOpen}
                onToggleSide={() => setSideOpen((open) => !open)}
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
                  {stage === "assets" && <AssetsArea ep={ep} />}
                  {stage === "studio" && <StudioArea ep={ep} onGo={nav.go} />}
                  {stage === "final" && <FinalArea ep={ep} onGo={nav.go} />}
                </main>
                {sideOpen && (
                  <SidePanel className="max-lg:absolute max-lg:inset-y-0 max-lg:right-0 max-lg:z-30 max-lg:shadow-xl" />
                )}
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
              onToggleSide={() => setSideOpen((open) => !open)}
            />
          </TooltipProvider>
        </MotionConfig>
      </DramaAssistantProvider>
    </DramaWorkspaceContext.Provider>
  );
}
