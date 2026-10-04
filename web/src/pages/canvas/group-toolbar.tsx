import { NodeToolbar, Position, useReactFlow, useStore } from "@xyflow/react";
import { motion } from "motion/react";
import { LayoutGrid, Trash2, Ungroup } from "lucide-react";

import {
  ChromeButton,
  ChromePill,
  ChromeSeparator,
  ChromeTooltip,
  Kbd,
} from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { CanvasEdge, CanvasGroupData, FlowNode, GroupHue } from "@/types";
import { groupMembers, groupTitleScale } from "@/utils/canvas/group";

import { MOD } from "./chrome/keys";
import { useGroupUi } from "./group-ui";
import { useGroupOps } from "./use-group-ops";

/** 组名行高度、它离组框的间距（画布单位，随缩放变化）和工具条离组名行的间距（屏幕像素） */
const TITLE_HEIGHT = 28;
const TITLE_GAP = 8;
const TOOLBAR_GAP = 14;
/** 工具条的层级：高于选中节点（xyflow 给选中节点加 1000）和画布的 pane */
const TOOLBAR_Z_INDEX = 2000;

/** 可选的 8 种颜色，顺序即面板里的顺序；名字给 aria-label 用 */
const HUES: { hue: GroupHue; name: string }[] = [
  { hue: "red", name: "红" },
  { hue: "orange", name: "橙" },
  { hue: "yellow", name: "黄" },
  { hue: "green", name: "绿" },
  { hue: "cyan", name: "青" },
  { hue: "blue", name: "蓝" },
  { hue: "purple", name: "紫" },
  { hue: "pink", name: "粉" },
];

/** 一排色块：第一个是「无」，当前项外圈描边 */
function SwatchRow({
  label,
  current,
  tint,
  onPick,
}: {
  label: string;
  current: GroupHue | undefined;
  /** 背景色块要压淡，标签色块用原色 */
  tint: boolean;
  onPick: (hue: GroupHue | undefined) => void;
}) {
  return (
    <section aria-label={label} className="flex flex-col gap-2">
      <h4 className="text-muted-foreground px-0.5 text-xs">{label}</h4>
      <div className="grid grid-cols-5 gap-2">
        <button
          type="button"
          aria-label={`${label}：无`}
          aria-pressed={current === undefined}
          onClick={() => onPick(undefined)}
          className={cn(
            "ring-chrome-border focus-visible:ring-node-ring relative aspect-square rounded-[10px] ring-1 outline-none transition-transform duration-150 hover:scale-105 active:scale-95",
            current === undefined && "ring-node-ring ring-2 ring-offset-2 ring-offset-popover",
          )}
        >
          <span className="bg-muted-foreground absolute top-1/2 left-1/5 h-px w-3/5 -rotate-45" />
        </button>
        {HUES.map(({ hue, name }) => (
          <button
            key={hue}
            type="button"
            aria-label={`${label}：${name}`}
            aria-pressed={current === hue}
            onClick={() => onPick(hue)}
            style={{
              backgroundColor: tint
                ? `color-mix(in oklch, var(--group-${hue}) 38%, var(--popover))`
                : `var(--group-${hue})`,
            }}
            className={cn(
              "ring-chrome-border focus-visible:ring-node-ring aspect-square rounded-[10px] ring-1 outline-none transition-transform duration-150 hover:scale-105 active:scale-95",
              current === hue && "ring-node-ring ring-2 ring-offset-2 ring-offset-popover",
            )}
          />
        ))}
      </div>
    </section>
  );
}

/**
 * 选中组并「点一下」后浮在组名行上方的工具条（设计稿 6.10）：颜色 | 整理布局 | 解组 | 删除。
 * 改颜色、整理、解组都直接改节点表，撤销栈会自己发现并记成一步；删除要先弹确认框。
 */
export function GroupToolbar({ id, data }: { id: string; data: CanvasGroupData }) {
  const { getNodes, updateNodeData } = useReactFlow<FlowNode, CanvasEdge>();
  const ui = useGroupUi();
  // 工具条浮在组名行上方：组名行会反向放大（见 GroupTitle），偏移要把放大后的高度算进去
  const zoom = useStore((state) => state.transform[2]);
  const offset = (TITLE_HEIGHT * groupTitleScale(zoom) + TITLE_GAP) * zoom + TOOLBAR_GAP;

  const { arrangeGroup, ungroupById } = useGroupOps(ui);

  const memberCount = groupMembers(getNodes(), id).length;

  return (
    <NodeToolbar
      nodeId={id}
      isVisible
      position={Position.Top}
      offset={offset}
      align="center"
      // NodeToolbar 默认取「节点 z + 1」，组垫在最底下（z = -2000），工具条会被盖在画布的 pane 下面，
      // 看得见却点不到。显式抬到最上层
      style={{ zIndex: TOOLBAR_Z_INDEX }}
    >
      <motion.div
        initial={{ opacity: 0, y: 6, scale: 0.97 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        className="nodrag nopan"
      >
        <ChromePill size="lg" className="gap-0.5">
          <Popover>
            <PopoverTrigger render={<ChromeButton size="lg" aria-label="颜色" />}>
              <span
                aria-hidden
                className="size-4.5! rounded-full"
                style={{
                  background:
                    "conic-gradient(var(--group-red), var(--group-yellow), var(--group-green), var(--group-cyan), var(--group-blue), var(--group-purple), var(--group-red))",
                }}
              />
              <span className="text-foreground text-sm">颜色</span>
            </PopoverTrigger>
            <PopoverContent
              side="bottom"
              align="start"
              sideOffset={10}
              className="w-72 gap-4 p-3.5"
            >
              <SwatchRow
                label="背景颜色"
                tint
                current={data.color}
                onPick={(color) => updateNodeData(id, { color })}
              />
              <SwatchRow
                label="标签颜色"
                tint={false}
                current={data.labelColor}
                onPick={(labelColor) => updateNodeData(id, { labelColor })}
              />
            </PopoverContent>
          </Popover>
          <ChromeSeparator />
          <ChromeButton
            size="lg"
            aria-label="整理布局"
            disabled={memberCount < 2}
            onClick={() => arrangeGroup(id)}
          >
            <LayoutGrid />
            <span className="text-foreground text-sm">整理布局</span>
          </ChromeButton>
          <ChromeSeparator />
          <ChromeTooltip label="解散这个组，节点原地保留" shortcut={`⇧${MOD}G`}>
            <ChromeButton size="lg" aria-label="解组" onClick={() => ungroupById(id)}>
              <Ungroup />
              <span className="text-foreground text-sm">解组</span>
              <Kbd className="max-sm:hidden">⇧{MOD}G</Kbd>
            </ChromeButton>
          </ChromeTooltip>
          <ChromeSeparator />
          <ChromeTooltip label="删除组和组内节点" shortcut="⌫">
            <ChromeButton
              size="lg"
              aria-label="删除组"
              className="hover:text-destructive"
              onClick={() => ui.requestDelete(id)}
            >
              <Trash2 />
            </ChromeButton>
          </ChromeTooltip>
        </ChromePill>
      </motion.div>
    </NodeToolbar>
  );
}
