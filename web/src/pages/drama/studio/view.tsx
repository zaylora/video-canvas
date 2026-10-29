import { useEffect } from "react";
import { useDefaultLayout } from "react-resizable-panels";

import type { Drama } from "@/api/drama/type";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useMediaQuery } from "@/hooks/use-media-query";
import { studioColumns } from "@/utils/drama/studio-layout";

import { ClipBar } from "./clip-bar";
import { Editor } from "./editor";
import { Library } from "./library";
import { Preview } from "./preview";
import { StudioBar } from "./studio-bar";
import { PlaybackContext, usePlayback } from "./use-playback";
import { StudioContext, useStudio, useStudioState } from "./use-studio";

/**
 * 片场（工作台里的一道工序）：某一集的片段制作。
 * 顶部条 / 左资产库 / 中间大播放器 / 右检查器（参考素材、提示词）/ 底部时间线（含连播），各区之间可拖动调整大小。
 * 换一集由外层用 key 整块重来，不让上一集的状态残留。
 */
export function StudioView({
  drama,
  no,
  refresh,
}: {
  drama: Drama;
  no: number;
  refresh: () => Promise<void>;
}) {
  const state = useStudioState(drama, no, refresh);
  return (
    <StudioContext.Provider value={state}>
      <Shell />
    </StudioContext.Provider>
  );
}

/** 分栏线：平时是 1px 细线，悬停和拖动时亮成主题色，方便找到能拖的地方 */
const HANDLE =
  "hover:bg-primary/60 data-[separator=active]:bg-primary transition-colors duration-150";

/**
 * 连播状态（预览与时间线共用）和整体布局：
 * 上下两块（三栏区 / 时间线）、三栏之间（资产库 / 预览 / 编辑区）都能拖动调整大小，拖好的尺寸记在本机。
 */
function Shell() {
  const { segments, select } = useStudio();
  const playback = usePlayback(segments);

  // 连播切到哪个片段，选中（时间线描边、提示词编辑区）就跟到哪个
  useEffect(() => {
    if (playback.currentId) select(playback.currentId);
  }, [playback.currentId, select]);

  const lg = useMediaQuery("(min-width: 1024px)");
  const xl = useMediaQuery("(min-width: 1280px)");
  const columns = studioColumns({ lg, xl });
  // 拖好的尺寸记在本机；三栏在不同窗口宽度下显示的栏不同，各记一份
  const rowsLayout = useDefaultLayout({ id: "drama-studio-rows", panelIds: ["main", "clips"] });
  const columnsLayout = useDefaultLayout({ id: "drama-studio-columns", panelIds: columns });

  return (
    <PlaybackContext.Provider value={playback}>
      <div className="flex h-full min-h-0 flex-col">
        <StudioBar />
        <ResizablePanelGroup
          orientation="vertical"
          className="min-h-0 flex-1"
          defaultLayout={rowsLayout.defaultLayout}
          onLayoutChanged={rowsLayout.onLayoutChanged}
        >
          <ResizablePanel id="main" minSize={240}>
            <ResizablePanelGroup
              key={columns.join("|")}
              orientation="horizontal"
              defaultLayout={columnsLayout.defaultLayout}
              onLayoutChanged={columnsLayout.onLayoutChanged}
            >
              {columns.includes("library") && (
                <>
                  <ResizablePanel id="library" defaultSize={224} minSize={180} maxSize="35%">
                    <Library />
                  </ResizablePanel>
                  <ResizableHandle className={HANDLE} />
                </>
              )}
              {columns.includes("preview") && (
                <>
                  <ResizablePanel id="preview" minSize={280}>
                    <Preview />
                  </ResizablePanel>
                  <ResizableHandle className={HANDLE} />
                </>
              )}
              <ResizablePanel id="editor" defaultSize={400} minSize={320}>
                <Editor />
              </ResizablePanel>
            </ResizablePanelGroup>
          </ResizablePanel>
          <ResizableHandle className={HANDLE} />
          <ResizablePanel id="clips" defaultSize={132} minSize={112} maxSize="60%">
            <ClipBar />
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>
    </PlaybackContext.Provider>
  );
}
