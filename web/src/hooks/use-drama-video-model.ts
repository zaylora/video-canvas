import { useCallback, useMemo } from "react";

import type { ModelInfo } from "@/api/model/type";
import { useRemoteModels } from "@/hooks/use-models";
import { useStudioPrefs, useStudioPrefsStore } from "@/store/drama-studio";
import { pickVideoModel, storyboardMaxSeconds } from "@/utils/drama/video-models";

/**
 * 一部短剧当前所选的视频模型：分集页生成脚本（取时长上限作 max_seconds）和片段工作区生成视频共用，
 * 选择存在本机（按短剧分开）。存的模型已下线或不可用时回到第一个可用的。
 */
export function useDramaVideoModel(dramaId: string) {
  const remote = useRemoteModels("video");
  const prefs = useStudioPrefs(dramaId);
  const model: ModelInfo | undefined = useMemo(
    () => pickVideoModel(remote.models, prefs.videoModel),
    [prefs.videoModel, remote.models],
  );
  const setModel = useCallback(
    (key: string) => useStudioPrefsStore.getState().patch(dramaId, { videoModel: key }),
    [dramaId],
  );
  return {
    models: remote.models,
    status: remote.status,
    model,
    setModel,
    /** 生成分镜脚本时给后端的单段时长上限 */
    maxSeconds: storyboardMaxSeconds(model),
  };
}
