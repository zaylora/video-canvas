import { useCallback, useEffect } from "react";
import { useReactFlow, useUpdateNodeInternals } from "@xyflow/react";

import { NodeCard, NodeTextBody, type IncomingConnection } from "@/components/canvas";
import { NODE_META } from "@/constants/canvas";
import { useTextNode } from "@/hooks/use-text-node";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { canConnectKinds } from "@/utils/canvas/canvas";

import { NodeOverlays } from "./node-overlays";
import { useMultiSelected } from "./selection-toolbar";
import { NodeStatusLabel, TaskPromptPanel } from "./video-node";

/**
 * 文本节点：模型清单、参数与提交走后端生成任务，正文由任务回填写进 data.text。
 * 输入口随所选模型 input_schema 里带 port 的字段生成（上游文本可以接到 prompt 上），
 * 选中时下方浮出提示词与参数面板。
 */
export function TextCanvasNode({
  id,
  data,
  selected,
}: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
}) {
  const { getNode } = useReactFlow<CanvasNode>();
  const vm = useTextNode(id, data);
  const meta = NODE_META.get("script");
  const PlaceholderIcon = meta?.placeholderIcon;
  const KindIcon = meta?.icon;
  const status = data.status ?? "idle";
  const multiSelected = useMultiSelected();

  /** 输入口随模型 schema 增减，xyflow 要被通知重新测量，否则连线会因找不到 handle 被藏起来 */
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
      return !!from && canConnectKinds(from.data.kind, handleType, "script");
    },
    [getNode],
  );

  return (
    <>
      <NodeCard
        title={data.label}
        icon={KindIcon ? <KindIcon /> : undefined}
        status={
          <NodeStatusLabel
            view={{
              phase: status === "running" ? "running" : status === "error" ? "failed" : "idle",
            }}
          />
        }
        handles={vm.handles}
        canAcceptConnection={canAcceptConnection}
      >
        <NodeTextBody
          status={status}
          text={data.text}
          error={data.error}
          icon={PlaceholderIcon ? <PlaceholderIcon className="size-10" /> : null}
          placeholder="选中后输入要求生成文本"
        />
      </NodeCard>
      {selected && !multiSelected && (
        <NodeOverlays id={id} data={data} showHistory={false}>
          {(width) => (
            <TaskPromptPanel vm={vm} data={data} kind="script" nodeId={id} width={width} />
          )}
        </NodeOverlays>
      )}
    </>
  );
}
