import { Position, useReactFlow } from "@xyflow/react";

import { NodeCard, type NodeCardHandle } from "@/components/canvas";
import { BaseNodeContent } from "@/components/canvas/base-node";
import { DirectorNodeBody } from "@/components/director/director-node-body";
import { openDirector } from "@/components/director/open-director";
import { NODE_META } from "@/constants/canvas";
import type { CanvasNode, CanvasNodeData } from "@/types";

/**
 * 导演台只有一个隐藏的输出口：导出节点的来源线从这里出发。
 * 导演台不接上游、也不能直接当生成参考，所以不给可拉线的口。
 */
const DIRECTOR_HANDLES: NodeCardHandle[] = [
  { type: "source", position: Position.Right, hidden: true },
];

/**
 * 导演台节点（设计稿 docs/design/3D导演台 §6.2）：
 * 点正文里的「进入导演台」或双击节点进入全屏编辑器；选中时不出功能区和生成面板。
 * 编辑器全屏盖住画布，节点上不再标「编辑中」；进入的过程由加载层（DirectorHost）接管。
 */
export function DirectorCanvasNode({ id, data }: { id: string; data: CanvasNodeData }) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const KindIcon = NODE_META.get("director")?.icon;

  return (
    <NodeCard
      title={data.label}
      onRename={(label) => updateNodeData(id, { label })}
      icon={KindIcon ? <KindIcon /> : undefined}
      handles={DIRECTOR_HANDLES}
    >
      <BaseNodeContent>
        <DirectorNodeBody scene={data.scene} onEnter={() => openDirector(id, data)} />
      </BaseNodeContent>
    </NodeCard>
  );
}
