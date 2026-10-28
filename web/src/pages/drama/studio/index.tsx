import { useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import { MotionConfig } from "motion/react";

import type { Drama } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { usePageTitle } from "@/hooks/use-page-title";
import { formatPageTitle } from "@/utils/page-title";

import { AssistantPane } from "../assistant/assistant-pane";
import { DramaAssistantProvider } from "../assistant/use-drama-assistant";
import { useComposeNotices } from "../use-compose";
import { useDramaWorkspace } from "../workspace/use-workspace";
import { ClipBar } from "./clip-bar";
import { Editor } from "./editor";
import { Library } from "./library";
import { Preview } from "./preview";
import { Topbar } from "./topbar";
import { PlaybackContext, usePlayback } from "./use-playback";
import { StudioContext, useStudio, useStudioState } from "./use-studio";

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
 * 顶栏 / 左资产库 / 中间片段编辑 / 右预览（含成片）/ 底部片段条（含连播）；顶栏「助手」打开右侧的短剧助手抽屉。
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
      <DramaAssistantProvider drama={drama} refresh={refresh}>
        <MotionConfig reducedMotion="user">
          <TooltipProvider delay={400}>
            <Shell />
          </TooltipProvider>
        </MotionConfig>
      </DramaAssistantProvider>
    </StudioContext.Provider>
  );
}

/**
 * 工作区外壳：连播状态（预览与片段条共用）、助手抽屉。
 * 助手用右侧抽屉而不是另开页面：和工作台右栏是同一个 AssistantPane，宽屏挤占预览右侧，窄屏盖在上面。
 */
function Shell() {
  const { drama, segments, select } = useStudio();
  const playback = usePlayback(segments);
  const [assistantOpen, setAssistantOpen] = useState(false);
  useComposeNotices(drama.id);

  // 连播切到哪个片段，选中（片段条描边、提示词编辑区）就跟到哪个
  useEffect(() => {
    if (playback.currentId) select(playback.currentId);
  }, [playback.currentId, select]);

  return (
    <PlaybackContext.Provider value={playback}>
      <div className="bg-background text-foreground flex h-svh w-svw flex-col overflow-hidden">
        <Topbar
          assistantOpen={assistantOpen}
          onToggleAssistant={() => setAssistantOpen((open) => !open)}
        />
        <div className="relative flex min-h-0 flex-1">
          <Library />
          <Editor />
          <Preview />
          {assistantOpen && (
            <aside
              aria-label="短剧助手"
              className="border-border bg-background flex w-80 shrink-0 flex-col border-l max-lg:absolute max-lg:inset-y-0 max-lg:right-0 max-lg:z-30 max-lg:shadow-xl"
            >
              <div className="min-h-0 flex-1">
                <AssistantPane />
              </div>
            </aside>
          )}
        </div>
        <ClipBar />
      </div>
    </PlaybackContext.Provider>
  );
}
