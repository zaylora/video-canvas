import { useCallback, useEffect } from "react";
import { NodeToolbar, Position, useReactFlow, useUpdateNodeInternals } from "@xyflow/react";

import {
  NodeCard,
  NodePromptInput,
  NodeVideoBody,
  VideoParamPanel,
  type IncomingConnection,
} from "@/components/canvas";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { NODE_META } from "@/constants/canvas";
import type { TaskNodeModel } from "@/hooks/use-task-node";
import { useTaskGeneration } from "@/hooks/use-task-generation";
import { useTaskNode } from "@/hooks/use-task-node";
import type { CanvasNode, CanvasNodeData, NodeKind } from "@/types";
import { canConnectKinds } from "@/utils/canvas/canvas";

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

/**
 * 节点下方的提示词 + 参数面板，跟着 NodeToolbar 装卸，没选中的节点不渲染。
 * 四种生成节点共用，kind 决定占位提示与图标。
 */
export function TaskPromptPanel({
  vm,
  data,
  kind,
}: {
  vm: TaskNodeModel;
  data: CanvasNodeData;
  kind: NodeKind;
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
      ? {
          tone: "error" as const,
          text: "这个模型已经下线，请在下拉里换一个模型后再生成",
        }
      : null;

  return (
    <>
      <NodePromptInput
        value={typeof vm.params.prompt === "string" ? vm.params.prompt : ""}
        onValueChange={vm.setPrompt}
        placeholder={meta?.placeholder}
        icon={Icon ? <Icon className="size-4" /> : undefined}
        models={vm.modelOptions}
        modelId={vm.modelKey ?? ""}
        onModelChange={vm.setModel}
        modelLabel={modelLabel}
        modelInvalid={vm.offline || modelsStatus === "error"}
        credits={vm.model?.credits}
        availableCredits={vm.availableCredits}
        running={vm.running}
        submitting={vm.submitting}
        onSubmit={() => void vm.submit()}
        canSubmit={!vm.blockedReason}
        hint={vm.blockedReason ?? "开始生成"}
        hidePrompt={!vm.hasPromptField}
        promptDisabled={!!vm.promptBinding}
        promptNote={vm.promptBinding ? `由上游「${vm.promptBinding.sourceLabel}」提供` : undefined}
        notice={notice}
      >
        {vm.schema && (
          <VideoParamPanel
            schema={vm.schema}
            params={vm.params}
            paramAssets={data.paramAssets}
            bindings={vm.bindings}
            errors={vm.errors}
            showErrors
            disabled={vm.running || vm.submitting}
            onChange={vm.setParam}
            listAssets={vm.listAssets}
          />
        )}
      </NodePromptInput>
      <SwitchModelDialog vm={vm} />
    </>
  );
}

/**
 * 媒体生成节点（图片、视频、音频共用）：正文按任务状态（排队 / 生成中 / 转存中 / 成功 / 失败）渲染，
 * 模型清单与输入口由后端下发的模型 input_schema 决定（带 port 的字段生成输入口），
 * 选中时下方浮出提示词与参数面板。种类在节点整个生命周期里不变，hook 集合不会切换。
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
      return !!from && canConnectKinds(from.data.kind, handleType, kind);
    },
    [getNode, kind],
  );

  const retryable = vm.view.phase === "failed";

  return (
    <>
      <NodeCard title={data.label} handles={vm.handles} canAcceptConnection={canAcceptConnection}>
        <NodeVideoBody
          view={vm.view}
          caption={data.fileName}
          placeholder={meta?.description ?? kind}
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
      <NodeToolbar isVisible={selected} position={Position.Bottom} offset={16}>
        <TaskPromptPanel vm={vm} data={data} kind={kind} />
      </NodeToolbar>
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
