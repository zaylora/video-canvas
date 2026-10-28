import { Link, useParams } from "react-router";
import { MotionConfig } from "motion/react";

import type { Drama } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { usePageTitle } from "@/hooks/use-page-title";
import { formatPageTitle } from "@/utils/page-title";

import { useDramaWorkspace } from "../workspace/use-workspace";
import { ClipBar } from "./clip-bar";
import { Editor } from "./editor";
import { Library } from "./library";
import { Preview } from "./preview";
import { Topbar } from "./topbar";
import { StudioContext, useStudioState } from "./use-studio";

function StudioSkeleton() {
  return (
    <div className="flex h-svh w-svw flex-col">
      <Skeleton className="h-12 w-full rounded-none motion-reduce:animate-none" />
      <div className="flex min-h-0 flex-1">
        <Skeleton className="h-full w-60 rounded-none motion-reduce:animate-none" />
        <div className="flex-1 p-8">
          <Skeleton className="h-8 w-48 motion-reduce:animate-none" />
          <Skeleton className="mt-6 h-48 w-full motion-reduce:animate-none" />
        </div>
      </div>
    </div>
  );
}

function Missing({ text, dramaId }: { text: string; dramaId?: string }) {
  return (
    <div className="grid h-svh place-items-center">
      <div className="flex flex-col items-center gap-3 text-sm">
        <p>{text}</p>
        <Button render={<Link to={dramaId ? `/drama/${dramaId}?area=episodes` : "/drama"} />}>
          {dramaId ? "返回分集视频" : "返回短剧列表"}
        </Button>
      </div>
    </div>
  );
}

/**
 * 片段工作区（/drama/:id/ep/:no，全屏，与短剧工作台同级）：
 * 顶栏 / 左资产库 / 中间片段编辑 / 右预览 / 底部片段条。
 * 换一集整页重来，不让上一集的状态残留。
 */
export default function DramaStudioPage() {
  const { id = "", no = "" } = useParams();
  return <Studio key={`${id}:${no}`} id={id} no={Number(no)} />;
}

function Studio({ id, no }: { id: string; no: number }) {
  const { drama, error, refresh } = useDramaWorkspace(id);
  usePageTitle(formatPageTitle(drama?.title, "片段工作区"));
  if (error && !drama) {
    return <Missing text={error === "notfound" ? "这部短剧不存在或已被删除" : "短剧加载失败"} />;
  }
  if (!drama) return <StudioSkeleton />;
  if (!Number.isInteger(no) || !drama.episodes.some((ep) => ep.no === no)) {
    return <Missing text="这一集不存在" dramaId={drama.id} />;
  }
  return <Ready drama={drama} no={no} refresh={refresh} />;
}

function Ready({ drama, no, refresh }: { drama: Drama; no: number; refresh: () => Promise<void> }) {
  const state = useStudioState(drama, no, refresh);
  return (
    <StudioContext.Provider value={state}>
      <MotionConfig reducedMotion="user">
        <TooltipProvider delay={400}>
          <div className="bg-background text-foreground flex h-svh w-svw flex-col overflow-hidden">
            <Topbar />
            <div className="flex min-h-0 flex-1">
              <Library />
              <Editor />
              <Preview />
            </div>
            <ClipBar />
          </div>
        </TooltipProvider>
      </MotionConfig>
    </StudioContext.Provider>
  );
}
