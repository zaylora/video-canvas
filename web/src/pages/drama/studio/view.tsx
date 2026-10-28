import { useEffect } from "react";

import type { Drama } from "@/api/drama/type";

import { ClipBar } from "./clip-bar";
import { Editor } from "./editor";
import { Library } from "./library";
import { Preview } from "./preview";
import { StudioBar } from "./studio-bar";
import { PlaybackContext, usePlayback } from "./use-playback";
import { StudioContext, useStudio, useStudioState } from "./use-studio";

/**
 * 片场（工作台里的一道工序）：某一集的片段制作。
 * 顶部条 / 左资产库 / 中间大播放器 / 右检查器（参考素材、提示词、分镜）/ 底部时间线（含连播）。
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

/** 连播状态（预览与时间线共用）和整体布局 */
function Shell() {
  const { segments, select } = useStudio();
  const playback = usePlayback(segments);

  // 连播切到哪个片段，选中（时间线描边、提示词编辑区）就跟到哪个
  useEffect(() => {
    if (playback.currentId) select(playback.currentId);
  }, [playback.currentId, select]);

  return (
    <PlaybackContext.Provider value={playback}>
      <div className="flex h-full min-h-0 flex-col">
        <StudioBar />
        <div className="flex min-h-0 flex-1">
          <Library />
          <Preview />
          <Editor />
        </div>
        <ClipBar />
      </div>
    </PlaybackContext.Provider>
  );
}
