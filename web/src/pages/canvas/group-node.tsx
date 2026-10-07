import { useAgentHighlight } from "@/store/agent-highlight";
import { memo, useCallback, useEffect, useRef } from "react";
import {
  NodeResizer,
  useReactFlow,
  useStore,
  type NodeProps,
  type ReactFlowState,
} from "@xyflow/react";
import { LayoutGrid } from "lucide-react";

import { cn } from "@/lib/utils";
import type { CanvasEdge, CanvasGroupNode, FlowNode } from "@/types";
import { GROUP_MIN, canResizeGroup, groupTitleScale, membersBounds } from "@/utils/canvas/group";
import { NODE_LABEL_MAX, normalizeNodeLabel } from "@/utils/canvas/node-label";

import { GroupToolbar } from "./group-toolbar";
import { useGroupUi } from "./group-ui";

/** 画布上恰好只选中了一个节点 */
const onlyOneSelected = (state: ReactFlowState) => {
  let count = 0;
  for (const node of state.nodes) if (node.selected && ++count > 1) return false;
  return count === 1;
};

/** 组名行的放大倍数，按 0.1 取整：滚轮缩放时只在值真的变了才重渲染 */
const titleScaleOf = (state: ReactFlowState) =>
  Math.round(groupTitleScale(state.transform[2]) * 10) / 10;

/** 组名：双击（或选中后按 Enter / F2、刚打完组）原地变成输入框，Enter / 失焦提交，Esc 取消 */
function GroupTitle({ id, label }: { id: string; label: string }) {
  const { updateNodeData } = useReactFlow<FlowNode, CanvasEdge>();
  const ui = useGroupUi();
  const editing = ui.renamingId === id;
  const input = useRef<HTMLInputElement>(null);
  const finished = useRef(false);
  // 画布缩小时组名反向放大，屏幕上的字号保持可读；宽度也按倍数收窄，免得超出组框
  const scale = useStore(titleScaleOf);

  useEffect(() => {
    if (!editing) return;
    finished.current = false;
    input.current?.focus();
    input.current?.select();
  }, [editing]);

  const finish = (commit: boolean) => {
    if (finished.current) return;
    finished.current = true;
    if (commit && input.current) {
      const next = normalizeNodeLabel(input.current.value, label);
      if (next !== label) updateNodeData(id, { label: next });
    }
    ui.setRenamingId(null);
  };

  return (
    <div
      data-slot="group-title"
      onDoubleClick={() => ui.setRenamingId(id)}
      style={{
        transform: scale === 1 ? undefined : `scale(${scale})`,
        transformOrigin: "left bottom",
        maxWidth: `${100 / scale}%`,
      }}
      className="text-muted-foreground group-data-[labelled]/group:text-(--label) hover:bg-chrome-hover absolute bottom-[calc(100%+8px)] left-1 flex h-7 max-w-full items-center gap-2 rounded-lg px-1.5 text-[15px] font-semibold"
    >
      <LayoutGrid className="size-4.5 shrink-0" />
      {editing ? (
        <input
          ref={input}
          defaultValue={label}
          maxLength={NODE_LABEL_MAX}
          aria-label="组名"
          className="nodrag nopan bg-chrome text-foreground ring-node-ring h-6 w-36 rounded-md px-1.5 text-[15px] font-semibold ring-1 outline-none"
          onKeyDown={(event) => {
            event.stopPropagation();
            if (event.key === "Enter") finish(true);
            if (event.key === "Escape") finish(false);
          }}
          onBlur={() => finish(true)}
          onPointerDown={(event) => event.stopPropagation()}
        />
      ) : (
        <span className="truncate">{label}</span>
      )}
    </div>
  );
}

/**
 * 组节点（设计稿 6.10）：一个虚线圆角框垫在成员下面，框外左上方是组名。
 * 整个框内的空白处都能拖动，拖一下整组（成员跟着走）；点一下才弹工具条。
 * 选中时四角出缩放手柄；缩放不改变成员，也不能缩到比成员更小。
 */
export const GroupNodeView = memo(function GroupNodeView({
  id,
  data,
  selected,
}: NodeProps<CanvasGroupNode>) {
  const { getNodes } = useReactFlow<FlowNode, CanvasEdge>();
  const ui = useGroupUi();
  const single = useStore(onlyOneSelected);
  const menuOpen = selected && single && ui.menuId === id && ui.draggingId !== id;
  const { menuId, setMenuId } = ui;

  // 取消选中就把菜单标记清掉，免得之后只是拖动顺带选中时菜单又冒出来
  useEffect(() => {
    if (!selected && menuId === id) setMenuId(null);
  }, [id, menuId, selected, setMenuId]);

  /** 缩放这一步放不放行：边不能向内越过成员，规则见 canResizeGroup */
  const shouldResize = useCallback(
    (_: unknown, next: { x: number; y: number; width: number; height: number }) => {
      const nodes = getNodes();
      const self = nodes.find((node) => node.id === id);
      const bounds = membersBounds(nodes, id);
      if (!self || !bounds) return true;
      const { x, y } = self.position;
      return canResizeGroup(
        {
          x,
          y,
          width: self.width ?? self.measured?.width ?? 0,
          height: self.height ?? self.measured?.height ?? 0,
        },
        next,
        { x0: x + bounds.x0, y0: y + bounds.y0, x1: x + bounds.x1, y1: y + bounds.y1 },
      );
    },
    [getNodes, id],
  );

  const mark = useAgentHighlight((state) => state.marks[id]);
  const style = {
    "--hue": data.color ? `var(--group-${data.color})` : undefined,
    "--label": data.labelColor ? `var(--group-${data.labelColor})` : undefined,
  } as React.CSSProperties;

  return (
    <div
      className="group/group size-full"
      data-labelled={data.labelColor ? "" : undefined}
      style={style}
    >
      <NodeResizer
        isVisible={selected}
        minWidth={GROUP_MIN.width}
        minHeight={GROUP_MIN.height}
        shouldResize={shouldResize}
        handleClassName="size-2.5! rounded-[3px]! border-[1.5px]! border-node-ring! bg-card!"
        lineClassName="border-transparent!"
      />
      <div
        data-slot="group-frame"
        data-selected={selected ? "" : undefined}
        data-colored={data.color ? "" : undefined}
        data-agent-mark={mark}
        className={cn(
          "bg-foreground/8 border-foreground/20 size-full cursor-grab rounded-3xl border border-dashed",
          "transition-[background-color,border-color,box-shadow] duration-150",
          "data-colored:border-[color-mix(in_oklch,var(--hue)_40%,transparent)] data-colored:bg-[color-mix(in_oklch,var(--hue)_16%,transparent)]",
          "data-selected:border-node-ring data-selected:data-colored:border-node-ring",
          "data-[agent-mark=touched]:border-preset! data-[agent-mark=danger]:border-destructive!",
        )}
      />
      <GroupTitle id={id} label={data.label} />
      {menuOpen && <GroupToolbar id={id} data={data} />}
    </div>
  );
});
