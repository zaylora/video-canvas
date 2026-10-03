import { useCallback, useEffect } from "react";
import { ChevronDown, SlidersHorizontal } from "lucide-react";
import { useReactFlow, useUpdateNodeInternals } from "@xyflow/react";

import {
  NodeCard,
  NodePromptInput,
  NodeVideoBody,
  VideoParamPanel,
  type IncomingConnection,
} from "@/components/canvas";
import { PANEL_CHIP_CLASS } from "@/components/canvas/node-prompt-input";
import { OpTabs } from "@/components/canvas/op-tabs";
import type { GenerationOp } from "@/api/model/type";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { NODE_META } from "@/constants/canvas";
import type { TaskNodeModel } from "@/hooks/use-task-node";
import { useTaskGeneration } from "@/hooks/use-task-generation";
import { useTaskNode } from "@/hooks/use-task-node";
import type { CanvasNode, CanvasNodeData, NodeKind } from "@/types";
import { canLinkFrom } from "@/utils/canvas/link-rule";
import { OP_LABEL, openParams, paramSummary } from "@/utils/tasks/capabilities";
import type { VideoNodeView } from "@/utils/tasks/node-view";

import { NodeOverlays } from "./node-overlays";
import { useDragSelected } from "./overlay-gate";
import { useMultiSelected } from "./selection-toolbar";

/** 走「提交任务 -> 轮询 / 推送 -> 回填」流程的媒体节点种类 */
export type MediaTaskKind = "image" | "video" | "audio";

/** 换模型会丢参数时的确认框；挂在 Portal 里，事件别冒泡回节点 */
function SwitchModelDialog({ vm }: { vm: TaskNodeModel }) {
  const pending = vm.pendingSwitch;
  return (
    <Dialog open={!!pending} onOpenChange={(open) => !open && vm.cancelSwitch()}>
      <DialogContent
        onClick={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <DialogHeader>
          <DialogTitle>切换模型</DialogTitle>
          <DialogDescription>
            新模型没有这些参数，切换后会被丢弃：
            {pending?.droppedLabels.join("、")}。
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={vm.cancelSwitch}>
            取消
          </Button>
          <Button onClick={vm.confirmSwitch}>继续切换</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 节点标题行右侧的状态：生成中带进度，失败标红 */
export function NodeStatusLabel({
  view,
}: {
  view: Pick<VideoNodeView, "phase"> & { progress?: number | null };
}) {
  switch (view.phase) {
    case "queued":
      return <span className="text-muted-foreground">排队中</span>;
    case "running":
      return (
        <span className="text-status-running font-mono tabular-nums">
          生成中{typeof view.progress === "number" ? ` ${view.progress}%` : ""}
        </span>
      );
    case "finalizing":
      return <span className="text-status-running">即将完成</span>;
    case "failed":
      return <span className="text-destructive">生成失败</span>;
    default:
      return null;
  }
}

/**
 * 节点下方的生成面板（设计稿 6.4）：生成方式 Tabs、参考素材、提示词、
 * 底栏的模型 / 参数摘要 / 积分 + 发送。四种生成节点共用，kind 决定占位提示与图标。
 */
export function TaskPromptPanel({
  vm,
  data,
  kind,
  nodeId,
  width,
}: {
  vm: TaskNodeModel;
  data: CanvasNodeData;
  kind: NodeKind;
  nodeId: string;
  width: number;
}) {
  const meta = NODE_META.get(kind);
  const Icon = meta?.icon;
  const { modelsStatus, reloadModels } = vm;

  // 上次清单没拉下来的话，选中节点时顺手再试一次
  useEffect(() => {
    if (modelsStatus === "error") void reloadModels();
  }, [modelsStatus, reloadModels]);

  const modelLabel = vm.offline
    ? "模型已下线"
    : modelsStatus === "error"
      ? "模型加载失败"
      : modelsStatus === "ready" && !vm.model
        ? "暂无可用模型"
        : undefined;

  const notice = vm.submitError
    ? { tone: "error" as const, text: vm.submitError }
    : vm.offline
      ? { tone: "error" as const, text: "这个模型已经下线，请换一个模型后再生成" }
      : null;

  const locked = vm.running || vm.submitting;
  const ops = vm.caps?.ops ?? [];
  const params = openParams(vm.caps);
  const panelProps = vm.caps && {
    caps: vm.caps,
    op: vm.refOp,
    params: vm.params,
    paramAssets: data.paramAssets,
    bindings: vm.bindings,
    errors: vm.errors,
    showErrors: true,
    disabled: locked,
    onChange: vm.setParam,
    onAddRef: vm.addRef,
    onRemoveRef: vm.removeRef,
    listAssets: vm.listAssets,
  };

  return (
    <>
      <NodePromptInput
        width={width}
        value={typeof vm.params.prompt === "string" ? vm.params.prompt : ""}
        onValueChange={vm.setPrompt}
        placeholder={meta?.placeholder}
        icon={Icon ? <Icon className="size-4" /> : undefined}
        models={vm.modelOptions}
        modelId={vm.modelKey ?? ""}
        onModelChange={vm.setModel}
        modelLabel={modelLabel}
        modelInvalid={vm.offline || modelsStatus === "error"}
        credits={vm.price?.total}
        creditsIsMax={vm.price?.isMax}
        creditsDetail={vm.price?.detail}
        availableCredits={vm.availableCredits}
        running={vm.running}
        submitting={vm.submitting}
        onSubmit={() => void vm.submit()}
        canSubmit={!vm.blockedReason}
        hint={vm.blockedReason ?? "开始生成"}
        promptDisabled={!!vm.promptBinding}
        promptNote={vm.promptBinding ? `由上游「${vm.promptBinding.sourceLabel}」提供` : undefined}
        notice={notice}
        header={
          ops.length > 1 && !vm.autoOp ? (
            <OpTabs
              id={nodeId}
              value={vm.op}
              options={ops.map((op) => ({ value: op, label: OP_LABEL[op] }))}
              onValueChange={(op: GenerationOp) => vm.setOp(op)}
              disabled={locked}
            />
          ) : null
        }
        toolbarExtra={
          panelProps &&
          params.length > 0 && (
            <Popover>
              <PopoverTrigger className={PANEL_CHIP_CLASS} aria-label="生成参数">
                <SlidersHorizontal className="text-muted-foreground" />
                <span className="min-w-0 truncate">
                  {paramSummary(vm.caps, vm.params) || "参数"}
                </span>
                <ChevronDown className="opacity-50" />
              </PopoverTrigger>
              <PopoverContent
                side="top"
                align="start"
                sideOffset={10}
                className="nodrag nowheel w-80 rounded-xl p-3"
              >
                <VideoParamPanel {...panelProps} section="params" />
              </PopoverContent>
            </Popover>
          )
        }
      >
        {panelProps && vm.refKinds.length > 0 && <VideoParamPanel {...panelProps} section="refs" />}
      </NodePromptInput>
      <SwitchModelDialog vm={vm} />
    </>
  );
}

/**
 * 媒体生成节点（图片、视频、音频共用）：正文按任务状态（排队 / 生成中 / 转存中 / 成功 / 失败）渲染，
 * 模型清单、输入口与参数由后端下发的模型能力 capabilities 决定（提示词口 + 当前生成方式能接收的素材口），
 * 选中时上方浮出生成历史、下方浮出生成面板。种类在节点整个生命周期里不变，hook 集合不会切换。
 */
function MediaTaskNode({
  id,
  data,
  selected,
  kind,
}: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
  kind: MediaTaskKind;
}) {
  const { getNode } = useReactFlow<CanvasNode>();
  const generation = useTaskGeneration(id, kind);
  const vm = useTaskNode(id, data, kind, generation);
  const meta = NODE_META.get(kind);
  const PlaceholderIcon = meta?.placeholderIcon;
  const KindIcon = meta?.icon;

  // 输入口随所选模型的 schema 增减；xyflow 只在节点挂载时量一次连接点，
  // 之后口变了必须通知它重新测量，否则连到新口上的线会因为「找不到 handle」被藏起来
  const updateNodeInternals = useUpdateNodeInternals();
  const handleSignature = vm.handles
    .map((handle) => `${handle.type}:${handle.id ?? ""}:${handle.top ?? ""}`)
    .join("|");
  useEffect(() => {
    updateNodeInternals(id);
  }, [handleSignature, id, updateNodeInternals]);

  const canAcceptConnection = useCallback(
    ({ nodeId, handleType }: IncomingConnection) => {
      const from = getNode(nodeId);
      return !!from && canLinkFrom(from.data, handleType, data);
    },
    [data, getNode],
  );

  const retryable = vm.view.phase === "failed";
  const multiSelected = useMultiSelected();
  const dragSelected = useDragSelected(id);

  return (
    <>
      <NodeCard
        title={data.label}
        icon={KindIcon ? <KindIcon /> : undefined}
        status={<NodeStatusLabel view={vm.view} />}
        handles={vm.handles}
        canAcceptConnection={canAcceptConnection}
      >
        <NodeVideoBody
          view={vm.view}
          caption={data.fileName}
          placeholder={`选中后输入提示词生成${meta?.label ?? ""}`}
          mediaType={data.mediaType ?? kind}
          placeholderIcon={PlaceholderIcon ? <PlaceholderIcon className="size-10" /> : undefined}
          onCancel={
            vm.view.phase === "queued" || vm.view.phase === "running" ? vm.cancel : undefined
          }
          cancelling={vm.cancelling}
          onRetry={retryable ? () => void vm.submit() : undefined}
          retryDisabled={!!vm.blockedReason}
          retryHint={vm.blockedReason ?? undefined}
        />
      </NodeCard>
      {selected && !multiSelected && !dragSelected && (
        <NodeOverlays id={id} data={data} showHistory>
          {(width) => <TaskPromptPanel vm={vm} data={data} kind={kind} nodeId={id} width={width} />}
        </NodeOverlays>
      )}
    </>
  );
}

type MediaNodeProps = { id: string; data: CanvasNodeData; selected?: boolean };

/** 图片节点 */
export const ImageCanvasNode = (props: MediaNodeProps) => <MediaTaskNode {...props} kind="image" />;
/** 视频节点 */
export const VideoCanvasNode = (props: MediaNodeProps) => <MediaTaskNode {...props} kind="video" />;
/** 音频节点 */
export const AudioCanvasNode = (props: MediaNodeProps) => <MediaTaskNode {...props} kind="audio" />;
