import { useCallback, useState } from "react";
import { Position, useReactFlow } from "@xyflow/react";

import { NodeCard, NodeTextBody, type NodeCardHandle } from "@/components/canvas";
import { TextEditorDialog } from "@/components/canvas/text-editor-dialog";
import { NODE_META } from "@/constants/canvas";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { textEditPatch } from "@/utils/canvas/text-body";

import { NodeOverlays, NodeOverlaysPresence } from "./node-overlays";
import { useDragSelected } from "./overlay-gate";
import { useMultiSelected } from "./selection-toolbar";
import { NodeStatusLabel } from "./video-node";

/** 文本节点只有输出口：它是手写内容，不接上游，没有哪种节点能连进它 */
const TEXT_HANDLES: NodeCardHandle[] = [{ type: "source", position: Position.Right }];

/**
 * 文本节点：正文就是 data.text，按 Markdown 渲染，双击在弹窗里用富文本编辑。
 * 不调模型：没有提示词与参数面板，也没有输入口，选中时只浮出上方功能区；
 * 老数据里已经在跑的任务仍会回填正文，生成中锁定编辑。
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
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const meta = NODE_META.get("script");
  const PlaceholderIcon = meta?.placeholderIcon;
  const KindIcon = meta?.icon;
  const status = data.status ?? "idle";
  const multiSelected = useMultiSelected();
  const dragSelected = useDragSelected(id);

  /** 编辑弹窗是否打开：双击正文、工具栏「放大」都走它 */
  const [editing, setEditing] = useState(false);
  const openEditor = useCallback(() => setEditing(true), []);
  /** 生成中锁定：弹窗只能看，免得和即将回填的结果打架 */
  const editable = status !== "running";

  /** 弹窗关闭时写回正文：正文就是下游引用到的那份 */
  const onTextChange = useCallback(
    (draft: string) => {
      const patch = textEditPatch(data, draft);
      if (patch) updateNodeData(id, patch);
    },
    [data, id, updateNodeData],
  );

  return (
    <>
      <NodeCard
        title={data.label}
        onRename={(label) => updateNodeData(id, { label })}
        icon={KindIcon ? <KindIcon /> : undefined}
        status={
          <NodeStatusLabel
            view={{
              phase: status === "running" ? "running" : status === "error" ? "failed" : "idle",
            }}
          />
        }
        handles={TEXT_HANDLES}
      >
        <NodeTextBody
          status={status}
          text={data.text}
          error={data.error}
          taskRef={data.taskId ? data.errorTaskRef : null}
          icon={PlaceholderIcon ? <PlaceholderIcon className="size-10" /> : null}
          placeholder="双击输入文字"
          onEdit={editable ? openEditor : undefined}
        />
      </NodeCard>
      <TextEditorDialog
        open={editing}
        onOpenChange={setEditing}
        title={data.label}
        text={data.text ?? ""}
        editable={editable}
        onSave={onTextChange}
      />
      <NodeOverlaysPresence multiSelected={multiSelected}>
        {selected && !dragSelected && (
          <NodeOverlays key="overlays" id={id} data={data} onOpenText={openEditor} />
        )}
      </NodeOverlaysPresence>
    </>
  );
}
