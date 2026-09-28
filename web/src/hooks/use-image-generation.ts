import { useCallback, useEffect } from "react";
import { useReactFlow } from "@xyflow/react";

import demoImage from "@/assets/hero.png";
import { IMAGE_ESTIMATED_DURATION } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, NodeStatus } from "@/types";
import { releaseObjectUrl } from "@/utils/canvas/media";

/**
 * 图片节点的出图状态机：状态存在节点数据里，
 * 拖动、重渲染甚至反复折叠都不会把生成进度弄丢。
 */
export function useImageGeneration(
  id: string,
  status: NodeStatus,
  src?: string | null,
) {
  const { updateNodeData } = useReactFlow<CanvasNode, CanvasEdge>();

  // 重新出图会顶掉节点上原有那张，本地传进来的还得把地址还回去
  const run = useCallback(() => {
    releaseObjectUrl(src);
    updateNodeData(id, {
      status: "running",
      src: null,
      uploaded: false,
      fileName: undefined,
      mediaType: undefined,
    });
  }, [id, src, updateNodeData]);

  useEffect(() => {
    if (status !== "running") return;

    // 眼下只是按预估时长交付演示图，接真实出图接口时换成请求返回的 URL
    const timer = setTimeout(
      () => updateNodeData(id, { status: "done", src: demoImage }),
      IMAGE_ESTIMATED_DURATION,
    );

    return () => clearTimeout(timer);
  }, [id, status, updateNodeData]);

  return run;
}
