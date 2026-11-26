import type { EdgeTypes, NodeTypes } from "@xyflow/react";

import { AnimatedSvgEdge } from "@/components/canvas";

import { CanvasNodeView } from "./canvas-node";
import { GroupNodeView } from "./group-node";

export const CANVAS_MIN_ZOOM = 0.14;
export const canvasNodeTypes = { canvas: CanvasNodeView, group: GroupNodeView } satisfies NodeTypes;
export const canvasEdgeTypes = { animatedSvgEdge: AnimatedSvgEdge } satisfies EdgeTypes;
