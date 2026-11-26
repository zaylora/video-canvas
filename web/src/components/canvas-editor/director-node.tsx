import { lazy, Suspense, useState } from "react";
import { Position, useReactFlow } from "@xyflow/react";

import { NodeCard, type NodeCardHandle } from "@/components/canvas";
import { BaseNodeContent } from "@/components/canvas/base-node";
import { DirectorNodeBody } from "@/components/director/director-node-body";
import { openDirector } from "@/components/director/open-director";
import { NODE_META } from "@/constants/canvas";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { useSharedCanvas } from "@/utils/canvas/read-only";
import { NodeImage } from "@/components/canvas/node-image";
import { createDefaultScene } from "@/utils/director/scene";
import { supportsWebGL2 } from "@/utils/director/webgl";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

const SharedDirectorPreview = lazy(() => import("@/components/director/shared-director-preview"));

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
  const shared = useSharedCanvas();
  const [preview, setPreview] = useState(false);

  return (
    <NodeCard
      title={data.label}
      onRename={(label) => updateNodeData(id, { label })}
      icon={KindIcon ? <KindIcon /> : undefined}
      handles={DIRECTOR_HANDLES}
    >
      <BaseNodeContent>
        <DirectorNodeBody
          scene={data.scene}
          onEnter={() => (shared ? setPreview(true) : openDirector(id, data))}
        />
      </BaseNodeContent>
      {preview && (
        <Dialog open onOpenChange={(open) => !open && setPreview(false)}>
          <DialogContent
            className="sm:max-w-5xl"
            onDoubleClick={(event) => event.stopPropagation()}
          >
            <DialogHeader>
              <DialogTitle>{data.label}</DialogTitle>
            </DialogHeader>
            {supportsWebGL2() ? (
              <Suspense fallback={<p role="status">正在加载导演台…</p>}>
                <SharedDirectorPreview scene={data.scene ?? createDefaultScene()} />
              </Suspense>
            ) : (
              <div className="space-y-2">
                {data.scene?.thumb && (
                  <NodeImage
                    alt="已保存的场景"
                    src={data.scene.thumb.url}
                    className="max-h-[60dvh] w-full object-contain"
                  />
                )}
                <p role="status">当前浏览器不支持 3D（WebGL2），无法播放场景。</p>
              </div>
            )}
          </DialogContent>
        </Dialog>
      )}
    </NodeCard>
  );
}
