import { useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import { MotionConfig } from "motion/react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { usePageTitle } from "@/hooks/use-page-title";
import { formatPageTitle } from "@/utils/page-title";

import { AssetsArea } from "./assets-area";
import { BreakdownDialog } from "./breakdown-dialog";
import { DramaWorkspaceContext } from "./context";
import { EpisodesArea } from "./episodes-area";
import { Rail } from "./rail";
import { ScriptArea } from "./script-area";
import { SidePanel } from "./side-panel";
import { Topbar } from "./topbar";
import { useDramaWorkspace, type DramaWorkspace, type WorkspaceArea } from "./use-workspace";

const AREAS: readonly WorkspaceArea[] = ["script", "assets", "episodes"];

function WorkspaceSkeleton() {
  return (
    <div className="flex h-svh w-svw">
      <Skeleton className="h-full w-16 rounded-none motion-reduce:animate-none" />
      <div className="flex-1 p-6">
        <Skeleton className="h-8 w-60 motion-reduce:animate-none" />
        <Skeleton className="mt-6 h-64 w-full motion-reduce:animate-none" />
      </div>
    </div>
  );
}

/**
 * 短剧工作台（/drama/:id，全屏，与 /canvas/:id 同层级）：
 * 左导航（剧本 / 资产 / 分集）、顶栏（项目名、保存状态、画风、比例、文本模型）、内容区、右栏（进度 / 助手）。
 * 当前区写在地址栏（?area=assets），刷新能回到原处。
 */
export default function DramaWorkspacePage() {
  const { id = "" } = useParams();
  // 换一部短剧整页重来，不让上一部的状态残留
  return <Workspace key={id} id={id} />;
}

function Workspace({ id }: { id: string }) {
  const workspace = useDramaWorkspace(id);
  const [params, setParams] = useSearchParams();
  const requested = params.get("area") as WorkspaceArea | null;
  const area: WorkspaceArea = requested && AREAS.includes(requested) ? requested : "script";
  const [sideOpen, setSideOpen] = useState(() => window.innerWidth >= 1024);
  const [breakdownOpen, setBreakdownOpen] = useState(false);
  const { drama, error, jobs, save, refresh, patch, applyEpisode, reportSave } = workspace;

  usePageTitle(formatPageTitle(drama?.title, "短剧工作室"));

  const value = useMemo<DramaWorkspace | null>(
    () => (drama ? { drama, jobs, save, refresh, patch, applyEpisode, reportSave } : null),
    [drama, jobs, save, refresh, patch, applyEpisode, reportSave],
  );

  if (error && !drama) {
    return (
      <div className="grid h-svh place-items-center">
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
  if (!value) return <WorkspaceSkeleton />;

  return (
    <DramaWorkspaceContext.Provider value={value}>
      <MotionConfig reducedMotion="user">
        <TooltipProvider delay={400}>
          <div className="bg-background text-foreground flex h-svh w-svw overflow-hidden">
            <Rail
              area={area}
              onChange={(next) =>
                setParams(next === "script" ? {} : { area: next }, { replace: true })
              }
            />
            <div className="flex min-w-0 flex-1 flex-col">
              <Topbar sideOpen={sideOpen} onToggleSide={() => setSideOpen((open) => !open)} />
              <div className="flex min-h-0 flex-1">
                <main className="min-w-0 flex-1">
                  {area === "script" && (
                    <ScriptArea onBreakdownOpen={() => setBreakdownOpen(true)} />
                  )}
                  {area === "assets" && (
                    <AssetsArea onBreakdownOpen={() => setBreakdownOpen(true)} />
                  )}
                  {area === "episodes" && (
                    <EpisodesArea onGoScript={() => setParams({}, { replace: true })} />
                  )}
                </main>
                {sideOpen && (
                  <SidePanel className="max-lg:absolute max-lg:inset-y-0 max-lg:right-0 max-lg:z-30 max-lg:shadow-xl" />
                )}
              </div>
            </div>
          </div>
          <BreakdownDialog open={breakdownOpen} onOpenChange={setBreakdownOpen} />
        </TooltipProvider>
      </MotionConfig>
    </DramaWorkspaceContext.Provider>
  );
}
