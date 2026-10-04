import { createContext, useCallback, useContext } from "react";
import {
  NodeToolbar,
  Position,
  ViewportPortal,
  useReactFlow,
  useStore,
  type ReactFlowState,
  type XYPosition,
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
import type { CanvasEdge, CanvasNode, FlowNode, PendingGroup } from "@/types";
import { arrangeNodes, type ArrangeMode } from "@/utils/canvas/arrange";
import { absolutePosition, isGroupNode, localizePositions } from "@/utils/canvas/group";

import { animatePositions } from "./arrange-animation";
import { MOD } from "./chrome/keys";
import { SelectionFanHandle } from "./selection-fan-handle";
import { duplicateSelection } from "./use-canvas-shortcuts";

/** 选框比节点外沿多出的留白（画布单位），要包住卡片上方的标题行 */
const FRAME_PADDING = { x: 24, top: 44, bottom: 24 };
/** 是否选中了多个节点：多选时节点不再各自浮出面板，统一由选区工具条接管 */
const MultiSelectContext = createContext(false);
export const MultiSelectProvider = MultiSelectContext.Provider;
export const useMultiSelected = () => useContext(MultiSelectContext);

/** 选中的节点 id；选区里只要有组就返回空串，这时不出多选工具条（组有自己的菜单） */
const selectedKey = (state: ReactFlowState) => {
  const selected = state.nodes.filter((node) => node.selected);
  return selected.some((node) => node.type === "group")
    ? ""
    : selected.map((node) => node.id).join(",");
};

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

/** 选区的淡色底框：跟着画布缩放，不吃指针；右侧挂着「引用选中节点生成」的把手 */
function SelectionFrame({
  ids,
  onFanOut,
}: {
  ids: string[];
  onFanOut: (screen: XYPosition, group: PendingGroup) => void;
}) {
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
      <SelectionFanHandle
        ids={ids}
        x={x1 + FRAME_PADDING.x}
        y={(y0 - FRAME_PADDING.top + y1 + FRAME_PADDING.bottom) / 2}
        onFanOut={onFanOut}
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
export function SelectionToolbar({
  onFanOut,
  onGroup,
}: {
  /** 从选框右侧的「+」拉出或点击：交给画布弹种类菜单，引用被选中的节点 */
  onFanOut: (screen: XYPosition, group: PendingGroup) => void;
  /** 把选中的节点打成组 */
  onGroup: () => void;
}) {
  const key = useStore(selectedKey);
  const ids = key ? key.split(",") : [];
  const { getNodes, setNodes, setEdges, deleteElements, getEdges } = useReactFlow<
    FlowNode,
    CanvasEdge
  >();
  const zoom = useStore((state) => state.transform[2]);

  const arrange = useCallback(
    (mode: ArrangeMode) => {
      const all = getNodes();
      const selected = all.filter(
        (node): node is CanvasNode => node.selected === true && !isGroupNode(node),
      );
      // 成员的 position 是相对组的：先换成绝对位置排好，再换回各自的坐标系写回
      const flat = selected.map((node) => ({ ...node, position: absolutePosition(node, all) }));
      const targets = localizePositions(all, arrangeNodes(flat, mode));
      animatePositions({ getNodes, setNodes }, targets);
    },
    [getNodes, setNodes],
  );

  const download = useCallback(() => {
    const files = getNodes().flatMap((node) =>
      node.selected && node.type === "canvas" && node.data.src && !node.data.src.startsWith("blob:")
        ? [{ url: node.data.src, name: node.data.fileName ?? node.data.label }]
        : [],
    );
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
      <SelectionFrame ids={ids} onFanOut={onFanOut} />
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
            <ChromeTooltip label="打组" shortcut={`${MOD}G`}>
              <ChromeButton size="lg" aria-label="打组" onClick={onGroup}>
                <LayoutGrid />
                <span className="text-foreground text-sm">打组</span>
              </ChromeButton>
            </ChromeTooltip>
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
