import { MiniMap, ReactFlow, type ReactFlowProps } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { ComponentProps, ReactNode } from "react";

import { CanvasBackgroundLayer } from "@/components/canvas/canvas-background";
import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import { cn } from "@/lib/utils";
import { useSettingsStore } from "@/store";
import type { CanvasEdge, FlowNode } from "@/types";

import { CANVAS_MIN_ZOOM, canvasEdgeTypes, canvasNodeTypes } from "./renderers";

export function CanvasWorkspace({
  containerClassName,
  tool,
  aside,
  children,
  ...rootProps
}: ComponentProps<"div"> & { containerClassName?: string; tool?: string; aside?: ReactNode }) {
  return (
    <div className={cn("relative flex overflow-hidden", containerClassName ?? "h-svh w-svw")}>
      <div
        {...rootProps}
        data-tool={tool}
        data-canvas-root
        className="bg-canvas relative h-full min-w-0 flex-1 overflow-hidden"
      >
        {children}
      </div>
      {aside}
    </div>
  );
}

export function CanvasFlowSurface({
  minimap,
  children,
  ...props
}: ReactFlowProps<FlowNode, CanvasEdge> & { minimap: boolean }) {
  const background = useSettingsStore((state) => state.background);
  const wheelZoom = useSettingsStore((state) => state.wheelMode === "zoom");
  return (
    <ReactFlow
      {...props}
      proOptions={{ hideAttribution: true }}
      nodeTypes={canvasNodeTypes}
      edgeTypes={canvasEdgeTypes}
      defaultEdgeOptions={ANIMATED_EDGE_OPTIONS}
      minZoom={CANVAS_MIN_ZOOM}
      panOnScroll={!wheelZoom}
      zoomOnScroll={wheelZoom}
      zoomActivationKeyCode={wheelZoom ? null : ["Control", "Meta"]}
      zoomOnDoubleClick={false}
    >
      <CanvasBackgroundLayer background={background} />
      {children}
      {minimap && (
        <MiniMap
          position="bottom-left"
          pannable
          zoomable
          className="canvas-overlay-interactive !bottom-16 !left-[76px] overflow-hidden rounded-xl shadow-lg ring-1 ring-chrome-border"
          nodeColor="var(--muted-foreground)"
          nodeBorderRadius={12}
        />
      )}
    </ReactFlow>
  );
}
