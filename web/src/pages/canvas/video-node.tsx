import { useCallback, useEffect } from "react";
import {
  NodeToolbar,
  Position,
  useReactFlow,
  useUpdateNodeInternals,
} from "@xyflow/react";

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
import {
  useVideoNode,
  type VideoNodeModel,
} from "@/hooks/use-video-node";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { canConnectKinds } from "@/utils/canvas/canvas";

/** 换模型会丢参数时的确认框；挂在 Portal 里，事件别冒泡回节点 */
function SwitchModelDialog({ vm }: { vm: VideoNodeModel }) {
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

/** 节点下方的提示词 + 参数面板，跟着 NodeToolbar 装卸，没选中的节点不渲染 */
function VideoPromptPanel({
  vm,
  data,
}: {
  vm: VideoNodeModel;
  data: CanvasNodeData;
}) {
  const meta = NODE_META.get("video");
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
        promptNote={
          vm.promptBinding ? `由上游「${vm.promptBinding.sourceLabel}」提供` : undefined
        }
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
 * 视频节点：正文按任务状态（排队 / 生成中 / 转存中 / 成功 / 失败）渲染，
 * 输入口由所选模型 input_schema 里带 port 的字段生成，
 * 选中时下方浮出提示词与参数面板。
 */
export function VideoCanvasNode({
  id,
  data,
  selected,
}: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
}) {
  const { getNode } = useReactFlow<CanvasNode>();
  const vm = useVideoNode(id, data);
  const meta = NODE_META.get("video");

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
      return !!from && canConnectKinds(from.data.kind, handleType, "video");
    },
    [getNode],
  );

  const retryable = vm.view.phase === "failed";

  return (
    <>
      <NodeCard
        title={data.label}
        handles={vm.handles}
        canAcceptConnection={canAcceptConnection}
      >
        <NodeVideoBody
          view={vm.view}
          caption={data.fileName}
          placeholder={meta?.description ?? "视频"}
          onCancel={vm.view.phase === "queued" || vm.view.phase === "running" ? vm.cancel : undefined}
          cancelling={vm.cancelling}
          onRetry={retryable ? () => void vm.submit() : undefined}
          retryDisabled={!!vm.blockedReason}
          retryHint={vm.blockedReason ?? undefined}
        />
      </NodeCard>
      <NodeToolbar isVisible={selected} position={Position.Bottom} offset={16}>
        <VideoPromptPanel vm={vm} data={data} />
      </NodeToolbar>
    </>
  );
}
