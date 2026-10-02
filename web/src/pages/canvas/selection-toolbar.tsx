import { createContext, useCallback, useContext } from "react";
import {
  NodeToolbar,
  Position,
  ViewportPortal,
  useReactFlow,
  useStore,
  type ReactFlowState,
} from "@xyflow/react";
import { motion } from "motion/react";
import {
  ChevronDown,
  Columns3,
  Copy,
  Download,
  Grid2x2,
  LayoutGrid,
  Rows3,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";

import {
  ChromeButton,
  ChromePill,
  ChromeSeparator,
  ChromeTooltip,
} from "@/components/canvas/chrome/chrome";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DURATION, EASE_OUT } from "@/lib/motion";
import type { CanvasEdge, CanvasNode } from "@/types";
import { arrangeNodes, type ArrangeMode } from "@/utils/canvas/arrange";

import { MOD } from "./chrome/keys";
import { duplicateSelection } from "./use-canvas-shortcuts";

/** 选框比节点外沿多出的留白（画布单位），要包住卡片上方的标题行 */
const FRAME_PADDING = { x: 24, top: 44, bottom: 24 };
/** 整理布局的过渡时长，毫秒 */
const ARRANGE_DURATION = 260;

/** 是否选中了多个节点：多选时节点不再各自浮出面板，统一由选区工具条接管 */
const MultiSelectContext = createContext(false);
export const MultiSelectProvider = MultiSelectContext.Provider;
export const useMultiSelected = () => useContext(MultiSelectContext);

const selectedKey = (state: ReactFlowState) =>
  state.nodes
    .filter((node) => node.selected)
    .map((node) => node.id)
    .join(",");

/** 选中节点的包围盒（画布坐标），拼成字符串让选择器只在真的变了时触发重渲染 */
const boundsKey = (state: ReactFlowState) => {
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (const node of state.nodeLookup.values()) {
    if (!node.selected) continue;
    const { x, y } = node.internals.positionAbsolute;
    x0 = Math.min(x0, x);
    y0 = Math.min(y0, y);
    x1 = Math.max(x1, x + (node.measured.width ?? 0));
    y1 = Math.max(y1, y + (node.measured.height ?? 0));
  }
  return Number.isFinite(x0) ? [x0, y0, x1, y1].map(Math.round).join(",") : "";
};

/** 选区的淡色底框：跟着画布缩放，不吃指针 */
function SelectionFrame() {
  const key = useStore(boundsKey);
  if (!key) return null;
  const [x0, y0, x1, y1] = key.split(",").map(Number);
  return (
    <ViewportPortal>
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        className="bg-foreground/[0.035] ring-foreground/15 pointer-events-none absolute rounded-3xl ring-1"
        style={{
          transform: `translate(${x0 - FRAME_PADDING.x}px, ${y0 - FRAME_PADDING.top}px)`,
          width: x1 - x0 + FRAME_PADDING.x * 2,
          height: y1 - y0 + FRAME_PADDING.top + FRAME_PADDING.bottom,
        }}
      />
    </ViewportPortal>
  );
}

/** 把一个地址存成文件：同源直接下，跨域先取成 blob 再下，都不行就新开标签页 */
async function downloadUrl(url: string, name: string) {
  try {
    const blob = await fetch(url).then((response) => {
      if (!response.ok) throw new Error(String(response.status));
      return response.blob();
    });
    const href = URL.createObjectURL(blob);
    const link = Object.assign(document.createElement("a"), { href, download: name });
    link.click();
    window.setTimeout(() => URL.revokeObjectURL(href), 1000);
  } catch {
    window.open(url, "_blank", "noopener");
  }
}

const ARRANGE_OPTIONS: { mode: ArrangeMode; label: string; icon: typeof Rows3 }[] = [
  { mode: "row", label: "横向排列", icon: Columns3 },
  { mode: "column", label: "纵向排列", icon: Rows3 },
  { mode: "grid", label: "网格排列", icon: Grid2x2 },
];

/**
 * 多选时的选区工具条（参考 neoWow）：浮在选区上方，不随缩放变化。
 * 节点数 | 整理布局 | 复制 | 下载 | 删除。
 */
export function SelectionToolbar() {
  const key = useStore(selectedKey);
  const ids = key ? key.split(",") : [];
  const { getNodes, setNodes, setEdges, deleteElements, getEdges } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const zoom = useStore((state) => state.transform[2]);

  const arrange = useCallback(
    (mode: ArrangeMode) => {
      const selected = getNodes().filter((node) => node.selected);
      const targets = arrangeNodes(selected, mode);
      const from = new Map(selected.map((node) => [node.id, node.position]));
      const start = performance.now();
      // 过渡期间标成 dragging，撤销栈只在落定时记一步
      const step = (now: number) => {
        const t = Math.min(1, (now - start) / ARRANGE_DURATION);
        const eased = 1 - Math.pow(1 - t, 3);
        setNodes((nodes) =>
          nodes.map((node) => {
            const to = targets.get(node.id);
            const origin = from.get(node.id);
            if (!to || !origin) return node;
            return {
              ...node,
              dragging: t < 1,
              position: {
                x: origin.x + (to.x - origin.x) * eased,
                y: origin.y + (to.y - origin.y) * eased,
              },
            };
          }),
        );
        if (t < 1) requestAnimationFrame(step);
      };
      requestAnimationFrame(step);
    },
    [getNodes, setNodes],
  );

  const download = useCallback(() => {
    const files = getNodes()
      .filter((node) => node.selected && node.data.src && !node.data.src.startsWith("blob:"))
      .map((node) => ({
        url: node.data.src as string,
        name: node.data.fileName ?? node.data.label,
      }));
    if (files.length === 0) {
      toast.info("选中的节点里还没有可下载的素材");
      return;
    }
    for (const file of files) void downloadUrl(file.url, file.name);
  }, [getNodes]);

  const duplicate = useCallback(
    () => duplicateSelection({ getNodes, getEdges, setNodes, setEdges }),
    [getEdges, getNodes, setEdges, setNodes],
  );

  const remove = useCallback(() => {
    const nodes = getNodes().filter((node) => node.selected);
    const idSet = new Set(nodes.map((node) => node.id));
    const edges = getEdges().filter((edge) => idSet.has(edge.source) || idSet.has(edge.target));
    void deleteElements({ nodes, edges });
    toast(`已删除 ${nodes.length} 个节点`, { description: `${MOD}Z 可以恢复` });
  }, [deleteElements, getEdges, getNodes]);

  if (ids.length < 2) return null;

  return (
    <>
      <SelectionFrame />
      <NodeToolbar
        nodeId={ids}
        isVisible
        position={Position.Top}
        offset={FRAME_PADDING.top * zoom + 14}
      >
        <motion.div
          initial={{ opacity: 0, y: 6, scale: 0.97 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          className="nodrag nopan"
        >
          <ChromePill size="lg" className="gap-0.5">
            <span className="text-muted-foreground px-3 text-sm tabular-nums">
              {ids.length} 个节点
            </span>
            <ChromeSeparator />
            <DropdownMenu modal={false}>
              <DropdownMenuTrigger render={<ChromeButton size="lg" aria-label="整理布局" />}>
                <LayoutGrid />
                <span className="text-foreground text-sm">整理布局</span>
                <ChevronDown className="size-3.5! opacity-60" />
              </DropdownMenuTrigger>
              <DropdownMenuContent side="bottom" align="start" sideOffset={10} className="w-40">
                {ARRANGE_OPTIONS.map((option) => (
                  <DropdownMenuItem key={option.mode} onClick={() => arrange(option.mode)}>
                    <option.icon />
                    {option.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <ChromeSeparator />
            <ChromeTooltip label="复制" shortcut={`${MOD}D`}>
              <ChromeButton size="lg" aria-label="复制选中节点" onClick={duplicate}>
                <Copy />
              </ChromeButton>
            </ChromeTooltip>
            <ChromeTooltip label="下载素材">
              <ChromeButton size="lg" aria-label="下载素材" onClick={download}>
                <Download />
              </ChromeButton>
            </ChromeTooltip>
            <ChromeTooltip label="删除" shortcut="⌫">
              <ChromeButton
                size="lg"
                aria-label="删除选中节点"
                className="hover:text-destructive"
                onClick={remove}
              >
                <Trash2 />
              </ChromeButton>
            </ChromeTooltip>
          </ChromePill>
        </motion.div>
      </NodeToolbar>
    </>
  );
}
