import { Position, useReactFlow } from "@xyflow/react";
import { Video } from "lucide-react";

import { NodeCard, type NodeCardHandle } from "@/components/canvas";
import { BaseNodeContent } from "@/components/canvas/base-node";
import { NODE_META } from "@/constants/canvas";
import { useDirectorStore } from "@/store/director";
import type { CanvasNode, CanvasNodeData } from "@/types";

import { NodeOverlays, NodeOverlaysPresence } from "./node-overlays";
import { useDragSelected } from "./overlay-gate";
import { useMultiSelected } from "./selection-toolbar";

/**
 * 导演台只有一个隐藏的输出口：导出节点的来源线从这里出发。
 * 导演台不接上游、也不能直接当生成参考，所以不给可拉线的口。
 */
const DIRECTOR_HANDLES: NodeCardHandle[] = [
  { type: "source", position: Position.Right, hidden: true },
];

/**
 * 导演台节点（设计稿 docs/design/3D导演台 §6.2）：
 * 正文是当前机位的缩略图和机位列表，双击或功能区「打开导演台」进入全屏编辑器。
 * 这里不加载 three.js，缩略图是关闭编辑器时渲染上传的图片。
 */
export function DirectorCanvasNode({
  id,
  data,
  selected,
}: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
}) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const meta = NODE_META.get("director");
  const KindIcon = meta?.icon;
  const PlaceholderIcon = meta?.placeholderIcon;
  const editing = useDirectorStore((state) => state.nodeId === id);
  const multiSelected = useMultiSelected();
  const dragSelected = useDragSelected(id);
  const scene = data.scene;
  const thumb = scene?.thumb?.url;

  return (
    <>
      <NodeCard
        title={data.label}
        onRename={(label) => updateNodeData(id, { label })}
        icon={KindIcon ? <KindIcon /> : undefined}
        status={editing ? <span className="text-status-running">编辑中</span> : undefined}
        handles={DIRECTOR_HANDLES}
      >
        <BaseNodeContent>
          <div className="bg-muted relative aspect-video w-full overflow-hidden rounded-[inherit]">
            {thumb ? (
              <img src={thumb} alt="" draggable={false} className="size-full object-contain" />
            ) : (
              <div className="text-muted-foreground flex size-full flex-col items-center justify-center gap-2 text-xs">
                {PlaceholderIcon && <PlaceholderIcon className="size-10 opacity-60" />}
                双击打开导演台
              </div>
            )}
            {scene && scene.cameras.length > 0 && (
              <div className="pointer-events-none absolute right-2 bottom-2 left-2 flex flex-wrap gap-1">
                {scene.cameras.map((camera) => (
                  <span
                    key={camera.id}
                    className="bg-background/80 text-foreground inline-flex h-5 items-center gap-1 rounded-full px-2 text-[11px] backdrop-blur-sm"
                  >
                    <Video className="size-3" />
                    {camera.name}
                    <span className="text-muted-foreground tabular-nums">
                      {scene.duration.toFixed(1)}s
                    </span>
                  </span>
                ))}
              </div>
            )}
          </div>
        </BaseNodeContent>
      </NodeCard>
      <NodeOverlaysPresence multiSelected={multiSelected}>
        {selected && !dragSelected && (
          <NodeOverlays key="overlays" id={id} data={data}>
            {() => null}
          </NodeOverlays>
        )}
      </NodeOverlaysPresence>
    </>
  );
}
