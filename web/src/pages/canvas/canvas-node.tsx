import { memo } from "react";
import type { NodeProps } from "@xyflow/react";

import type { CanvasNode } from "@/types";

import { TextCanvasNode } from "./text-node";
import { AudioCanvasNode, ImageCanvasNode, VideoCanvasNode } from "./video-node";

/**
 * 画布节点入口：四种节点都接真实生成任务（后端下发的模型清单、schema 驱动的参数面板、任务状态）。
 * 种类在节点整个生命周期里不变，分发不会切换 hook 集合。
 */
export const CanvasNodeView = memo((props: NodeProps<CanvasNode>) => {
  const { id, data, selected } = props;
  switch (data.kind) {
    case "image":
      return <ImageCanvasNode id={id} data={data} selected={selected} />;
    case "video":
      return <VideoCanvasNode id={id} data={data} selected={selected} />;
    case "audio":
      return <AudioCanvasNode id={id} data={data} selected={selected} />;
    case "script":
      return <TextCanvasNode id={id} data={data} selected={selected} />;
  }
});
CanvasNodeView.displayName = "CanvasNodeView";
