import { useEffect, useMemo, useState } from "react";
import { motion } from "motion/react";
import { NodeToolbar, Position, useReactFlow, useStore, type ReactFlowState } from "@xyflow/react";

import { NodePromptInput } from "@/components/canvas/node-prompt-input";
import { OutpaintFrameLayer } from "@/components/canvas/outpaint-frame-layer";
import { OutpaintRatioBar } from "@/components/canvas/outpaint-ratio-bar";
import {
  PANEL_OFFSET,
  usePaneBusy,
  usePanelPlacement,
} from "@/components/canvas/hooks/use-panel-placement";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { useImageSize } from "@/hooks/use-image-size";
import { toModelOption, useRemoteModels } from "@/hooks/use-models";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useCreditsStore } from "@/store/credits";
import { useOutpaintStore } from "@/store/outpaint";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { useCurrentCanvasId } from "@/utils/canvas/canvas-id";
import { planCompose } from "@/utils/canvas/outpaint-compose";
import {
  frameByMult,
  frameByRatio,
  ratioOfFrame,
  type OriginSize,
  type OutpaintFrame,
} from "@/utils/canvas/outpaint-frame";
import {
  buildOutpaintNode,
  buildOutpaintPrompt,
  outpaintModels,
  pickOutpaintModel,
  resolveRatioParam,
} from "@/utils/canvas/outpaint-node";
import { outpaintRunner } from "@/utils/canvas/outpaint-run";
import { fanoutCount, quote } from "@/utils/pricing/quote";
import { buildTaskInput, priceSpecOf } from "@/utils/tasks/capabilities";

/** 默认先扩到原比例的 1.5 倍（照原型） */
const DEFAULT_MULT = 1.5;

/** 参考图占位 id：只用来校验和计价，真正的合成图素材 id 上传后才有 */
const PREVIEW_IMAGE_ID = "1";

/**
 * 扩图模式（设计稿 6.17）：原图节点上画出可拖的框，下方是比例条 + 提示词栏（模型、积分、发送）。
 * 进入时只摆这些，不生成任何图片；点发送才建结果节点、合成、上传、提交。
 * 取代这个节点平时的功能区和生成面板。
 */
export function OutpaintOverlay({ id, data }: { id: string; data: CanvasNodeData }) {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const canvasId = useCurrentCanvasId();
  const history = useCanvasHistoryContext();
  const leave = useOutpaintStore((state) => state.leave);
  const { width, shift } = usePanelPlacement(id);
  const paneBusy = usePaneBusy();
  const zoom = useStore((state: ReactFlowState) => state.transform[2]);
  const availableCredits = useCreditsStore((state) => state.credits?.available ?? null);
  const remote = useRemoteModels("image");

  // 原图节点此刻的大小（画布坐标）：正文按宽高比撑开，标题行在卡片外，所以就是图的大小
  const sizeKey = useStore((state: ReactFlowState) => {
    const measured = state.nodeLookup.get(id)?.measured;
    return measured?.width && measured.height ? `${measured.width},${measured.height}` : "";
  });
  const origin = useMemo<OriginSize | null>(() => {
    if (!sizeKey) return null;
    const [w, h] = sizeKey.split(",").map(Number);
    return { w, h };
  }, [sizeKey]);

  const natural = useImageSize(data.src);
  const usable = useMemo(() => outpaintModels(remote.models), [remote.models]);
  const [modelKey, setModelKey] = useState<string | undefined>(
    () => pickOutpaintModel(remote.models, data.model)?.key,
  );
  const model = usable.find((item) => item.key === modelKey) ?? usable[0];
  const caps = model?.capabilities;

  const [prompt, setPrompt] = useState("");
  const [mult, setMult] = useState(DEFAULT_MULT);
  const [frame, setFrame] = useState<OutpaintFrame | null>(null);
  const currentFrame = frame ?? (origin ? frameByMult(origin, mult) : null);

  // Esc 退出
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") leave(id);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [id, leave]);

  // 比例、提示词、参数都定了之后的任务输入：用来校验和计价（合成图素材 id 先用占位）
  const preview = useMemo(() => {
    if (!caps || !origin || !currentFrame) return null;
    const ratio = resolveRatioParam(caps, ratioOfFrame(origin, currentFrame));
    const params: Record<string, unknown> = {
      prompt: buildOutpaintPrompt(prompt),
      op: "i2i",
      images: [PREVIEW_IMAGE_ID],
      ...(ratio.key ? { [ratio.key]: ratio.value } : {}),
    };
    return { ratio, built: buildTaskInput(caps, params, undefined, "i2i") };
  }, [caps, currentFrame, origin, prompt]);

  const price = useMemo(() => {
    if (!model || !preview) return null;
    const spec = priceSpecOf(caps, preview.built.input);
    const one = quote(model.pricing, caps, spec);
    const count = fanoutCount(caps, spec.params);
    return {
      total: one * count,
      isMax: model.pricing?.billing === "token",
      detail: model.pricing?.billing === "token" ? "按 Token 预估上限" : `${one} 积分`,
    };
  }, [caps, model, preview]);

  const firstError = preview ? Object.values(preview.built.errors)[0] : undefined;
  const lowCredits = !!price && availableCredits !== null && price.total > availableCredits;
  const blockedReason =
    remote.status === "loading" || remote.status === "idle"
      ? "正在加载模型清单"
      : !model
        ? "暂无支持图生图的模型"
        : !origin || !currentFrame || !data.src
          ? "原图还没有准备好"
          : (firstError ?? (lowCredits ? "积分不足" : null));

  if (!origin || !currentFrame) return null;

  const plan = natural && planCompose({ natural, origin, frame: currentFrame });
  const sizeLabel = plan ? `${plan.width} × ${plan.height}` : undefined;

  const pickMult = (next: number) => {
    setMult(next);
    setFrame(frameByMult(origin, next));
  };

  const submit = () => {
    if (blockedReason || !model || !caps || !preview || !data.src || !canvasId) return;
    const source = getNode(id);
    if (!source) return;
    // 建节点、连线算一步撤销；合成、上传、任务写回的字段撤销不倒回
    history?.record();
    const { node, edge } = buildOutpaintNode({
      source,
      nodes: getNodes(),
      model: model.key,
      prompt,
      ratio: preview.ratio.value,
      ratioKey: preview.ratio.key,
    });
    setNodes((nodes) => [...nodes, node]);
    setEdges((edges) => [...edges, edge]);
    leave(id);
    void outpaintRunner.start(
      {
        nodeId: node.id,
        sourceSrc: data.src,
        origin,
        frame: currentFrame,
        caps,
        modelKey: model.key,
        canvasId,
        params: node.data.params ?? {},
      },
      (nodeId, update) => updateNodeData(nodeId, (current) => update(current.data)),
    );
  };

  return (
    <>
      <OutpaintFrameLayer
        nodeId={id}
        origin={origin}
        frame={currentFrame}
        onChange={setFrame}
        sizeLabel={sizeLabel}
      />
      <NodeToolbar
        isVisible
        position={Position.Bottom}
        offset={(currentFrame.y1 - origin.h) * zoom + PANEL_OFFSET}
      >
        <motion.div
          initial={{ opacity: 0, y: -6, scale: 0.985 }}
          animate={{ opacity: paneBusy ? 0.35 : 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          style={{ translate: `${shift}px 0`, pointerEvents: paneBusy ? "none" : undefined }}
        >
          <NodePromptInput
            width={width}
            value={prompt}
            onValueChange={setPrompt}
            placeholder="描述你想如何修改图片（可以留空）"
            models={usable.map(toModelOption)}
            modelId={model?.key ?? ""}
            onModelChange={setModelKey}
            modelLabel={remote.status === "ready" && !model ? "暂无支持图生图的模型" : undefined}
            credits={price?.total}
            creditsIsMax={price?.isMax}
            creditsDetail={price?.detail}
            availableCredits={availableCredits}
            onSubmit={submit}
            canSubmit={!blockedReason}
            hint={blockedReason ?? "开始扩图"}
            promptMaxLength={caps?.prompt?.max_length}
            header={
              <OutpaintRatioBar
                origin={origin}
                frame={currentFrame}
                onPickMult={pickMult}
                onPickOriginal={() => pickMult(mult)}
                onPickRatio={(ratio) => setFrame(frameByRatio(origin, ratio))}
              />
            }
          />
        </motion.div>
      </NodeToolbar>
    </>
  );
}
