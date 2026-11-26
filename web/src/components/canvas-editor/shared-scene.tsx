import { useMemo, useState, type ReactNode } from "react";
import { ReactFlowProvider } from "@xyflow/react";

import type { CanvasGraphDto } from "@/api/canvas/type";
import { MediaLightbox, type LightboxTarget } from "@/components/canvas/media-lightbox";
import { TooltipProvider } from "@/components/ui/tooltip";
import { deserializeGraph } from "@/utils/canvas/canvas-persistence";
import { collectPreviewItems } from "@/utils/canvas/preview-items";
import { CanvasReadOnlyProvider, SharedCanvasProvider } from "@/utils/canvas/read-only";

import { NodePreviewProvider } from "./preview-context";
import { CanvasFlowSurface, CanvasWorkspace } from "./canvas-workspace";
import { useLocateNode } from "./use-focus-node";

const legacyMediaTargets = new Set(["image", "video", "audio", "character"]);

function sharedLayout(graph: CanvasGraphDto) {
  const layout = deserializeGraph(graph);
  const singleTarget = new Set(
    layout.nodes.flatMap((node) =>
      node.type === "canvas" && legacyMediaTargets.has(node.data.kind) ? [node.id] : [],
    ),
  );
  return {
    ...layout,
    edges: layout.edges.map((edge) =>
      edge.targetHandle && singleTarget.has(edge.target) ? { ...edge, targetHandle: null } : edge,
    ),
  };
}

type SharedSceneProps = {
  graph: CanvasGraphDto;
  overlay: (minimap: boolean, setMinimap: (open: boolean) => void) => ReactNode;
};

export function SharedCanvasScene(props: SharedSceneProps) {
  return (
    <ReactFlowProvider>
      <SharedSceneContent {...props} />
    </ReactFlowProvider>
  );
}

function SharedSceneContent({ graph, overlay }: SharedSceneProps) {
  const layout = useMemo(() => sharedLayout(graph), [graph]);
  const items = useMemo(() => collectPreviewItems(layout.nodes), [layout.nodes]);
  const [preview, setPreview] = useState<LightboxTarget | null>(null);
  const [minimap, setMinimap] = useState(false);
  const locate = useLocateNode();
  const openPreview = (id: string) => {
    if (items.some((item) => item.id === id)) setPreview({ id, origin: null });
  };

  return (
    <CanvasWorkspace>
      <CanvasReadOnlyProvider value>
        <SharedCanvasProvider value>
          <TooltipProvider delay={400}>
            <NodePreviewProvider value={openPreview}>
              <CanvasFlowSurface
                minimap={minimap}
                nodes={layout.nodes}
                edges={layout.edges}
                defaultViewport={layout.viewport}
                onNodeDoubleClick={(_event, node) => openPreview(node.id)}
                panOnDrag
                nodesDraggable={false}
                nodesConnectable={false}
                edgesReconnectable={false}
                elementsSelectable={false}
                disableKeyboardA11y
                deleteKeyCode={null}
              />
              {overlay(minimap, setMinimap)}
              <MediaLightbox
                items={items}
                target={preview}
                onActiveChange={openPreview}
                onLocate={locate}
                onClose={() => setPreview(null)}
              />
            </NodePreviewProvider>
          </TooltipProvider>
        </SharedCanvasProvider>
      </CanvasReadOnlyProvider>
    </CanvasWorkspace>
  );
}
