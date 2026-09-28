import { memo, useCallback, useMemo } from "react";
import {
  NodeToolbar,
  Position,
  useReactFlow,
  type NodeProps,
} from "@xyflow/react";

import {
  NodeCard,
  NodeImageBody,
  NodeMediaBody,
  NodePlaceholderBody,
  NodePromptInput,
  NodeTextBody,
  type IncomingConnection,
} from "@/components/canvas";

import {
  IMAGE_ESTIMATED_DURATION,
  NODE_LIBRARY,
  NODE_META,
} from "@/constants/canvas";
import { useImageGeneration } from "@/hooks/use-image-generation";
import { useTextGeneration } from "@/hooks/use-text-generation";
import { useSettingsStore } from "@/store";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { canConnectKinds, getModelOptions, pickModel } from "@/utils/canvas/canvas";

/** 图片节点的正文：把节点里存的出图状态摊给纯展示的 NodeImageBody。 */
function ImageNodeBody({ data }: { data: CanvasNodeData }) {
  // 出图还是演示状态机，跑不出 error；接真实服务时这里补上失败的样子
  const status = data.status ?? "idle";

  // 本地传进来的图已经在手上了，直接摆出来，别再演一遍出图的揭示动画
  if (data.uploaded && data.src) {
    return (
      <NodeMediaBody
        src={data.src}
        mediaType="image"
        alt={data.fileName ?? data.label}
        caption={data.fileName}
      />
    );
  }

  return (
    <NodeImageBody
      status={status === "error" ? "idle" : status}
      src={data.src}
      alt={data.label}
      estimatedDuration={IMAGE_ESTIMATED_DURATION}
    />
  );
}

type NodePromptPanelProps = {
  /** 节点 id，提示词和模型都写回这个节点 */
  id: string;
  /** 节点数据，提示词与选中的模型都在里面 */
  data: CanvasNodeData;
  /** 正在生成：发送按钮转圈并锁住 */
  running?: boolean;
  /** 不给就只存提示词，不跑生成 */
  onSubmit?: () => void;
  /** 没有 onSubmit 时，鼠标停在发送键上要给的说法 */
  submitHint?: string;
};

/**
 * 节点下方的提示词面板：把输入框接到节点数据上。
 * 提示词和模型都存在节点里，面板收起来再打开也还在。
 *
 * 单独拆成组件是为了让它跟着 NodeToolbar 一起装卸：
 * 没选中的节点不渲染这里，也就不订阅设置、不算模型清单。
 */
function NodePromptPanel({
  id,
  data,
  running,
  onSubmit,
  submitHint,
}: NodePromptPanelProps) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const customModels = useSettingsStore((state) => state.customModels);
  const meta = NODE_META.get(data.kind) ?? NODE_LIBRARY[0];
  const Icon = meta.icon;

  const models = useMemo(
    () => getModelOptions(data.kind, customModels),
    [customModels, data.kind],
  );

  const setPrompt = useCallback(
    (prompt: string) => updateNodeData(id, { prompt }),
    [id, updateNodeData],
  );

  const setModel = useCallback(
    (model: string) => updateNodeData(id, { model }),
    [id, updateNodeData],
  );

  return (
    <NodePromptInput
      value={data.prompt ?? ""}
      onValueChange={setPrompt}
      placeholder={meta.placeholder}
      icon={<Icon className="size-4" />}
      models={models}
      modelId={pickModel(models, data.model).id}
      onModelChange={setModel}
      running={running}
      onSubmit={onSubmit}
      submitHint={submitHint}
    />
  );
}

/** 画布节点：按种类挑正文，选中时节点下方浮出提示词输入框，外壳交给 NodeCard。 */
export const CanvasNodeView = memo(
  ({ id, data, selected }: NodeProps<CanvasNode>) => {
    const { getNode } = useReactFlow<CanvasNode>();
    const meta = NODE_META.get(data.kind) ?? NODE_LIBRARY[0];
    const PlaceholderIcon = meta.placeholderIcon;
    const isImage = data.kind === "image";
    const isText = data.kind === "script";
    const isVideo = data.kind === "video";
    const status = data.status ?? "idle";

    // hook 不能按种类跳过，所以两个都照挂，各自只认自己那种节点；
    // 视频和音频还没接生成服务，发送键点下去会说明还差什么
    const runImage = useImageGeneration(
      id,
      isImage && status !== "error" ? status : "idle",
      data.src,
    );
    const runText = useTextGeneration(id, data);
    const run = isImage ? runImage : isText ? runText : undefined;

    // 拉过来的线要按同一套规则过一遍，接不上就别沉下去骗人
    const canAcceptConnection = useCallback(
      ({ nodeId, handleType }: IncomingConnection) => {
        const from = getNode(nodeId);
        return !!from && canConnectKinds(from.data.kind, handleType, data.kind);
      },
      [data.kind, getNode],
    );

    return (
      <>
        <NodeCard title={data.label} canAcceptConnection={canAcceptConnection}>
          {isImage ? (
            <ImageNodeBody data={data} />
          ) : isText ? (
            <NodeTextBody
              status={status}
              text={data.text}
              error={data.error}
              icon={<PlaceholderIcon className="size-10" />}
              placeholder={meta.description}
            />
          ) : isVideo && data.src ? (
            // 片子眼下只能从本地传进来，接上视频生成服务后走的也是这条路
            <NodeMediaBody
              src={data.src}
              mediaType="video"
              caption={data.fileName}
            />
          ) : (
            // 还没出片的视频、以及还没接生成服务的音频，都先摆个占位框，
            // 卡片高度和图片节点对齐
            <NodePlaceholderBody
              icon={<PlaceholderIcon className="size-10" />}
              label={meta.description}
            />
          )}
        </NodeCard>
        {/*
         * 输入框吊在节点下方、选中才露面。
         * NodeToolbar 只按缩放换算位置、不缩放自身，
         * 于是画布怎么放大缩小，这块都保持一样大、一样好按。
         * 宽度不写死：它靠自身宽度居中，写窄了面板会整块偏出去
         */}
        <NodeToolbar isVisible={selected} position={Position.Bottom} offset={16}>
          <NodePromptPanel
            id={id}
            data={data}
            running={status === "running"}
            onSubmit={run}
            submitHint={`${meta.label}生成还没接入服务`}
          />
        </NodeToolbar>
      </>
    );
  },
);
CanvasNodeView.displayName = "CanvasNodeView";
