import { useMemo, useState } from "react";
import { FolderOpen, Hand, History, MousePointer2, Plus, Redo2, Undo2 } from "lucide-react";

import {
  ChromeButton,
  ChromePill,
  ChromeSegment,
  ChromeSeparator,
  ChromeTooltip,
} from "@/components/canvas/chrome/chrome";
import type { CanvasTool } from "@/components/canvas";
import { ADD_NODE_MENU_CONTENT_CLASS, AddNodeMenuBody } from "@/components/canvas/add-node-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ASSET_LIBRARY_ACTION, UPLOAD_ACTION } from "@/constants/canvas";
import type { NodeKind } from "@/types";

import { buildAddNodeItems } from "./add-node-items";
import { MOD } from "@/components/canvas-editor/keys";
import { TasksSheet } from "./tasks-sheet";

/**
 * 主工具条：添加节点、选择 / 抓手、撤销重做、资产库、任务历史。
 * 默认竖排贴左侧居中（无限画布）；horizontal 横排，放底部居中（短剧资产画布）。
 */
export function BottomToolbar({
  orientation = "vertical",
  tool,
  onToolChange,
  onAdd,
  onUpload,
  onOpenAssets,
  canUndo,
  canRedo,
  onUndo,
  onRedo,
}: {
  orientation?: "vertical" | "horizontal";
  tool: CanvasTool;
  onToolChange: (tool: CanvasTool) => void;
  onAdd: (kind: NodeKind) => void;
  onUpload: () => void;
  /** 打开资产库弹框：底栏按钮和「添加节点」菜单里的「从资产库添加」共用 */
  onOpenAssets: () => void;
  /** 打开角色卡选择框：「添加节点」菜单里的「角色卡」 */
  canUndo: boolean;
  canRedo: boolean;
  onUndo: () => void;
  onRedo: () => void;
}) {
  const [tasksOpen, setTasksOpen] = useState(false);
  const addItems = useMemo(() => buildAddNodeItems(), []);
  const vertical = orientation === "vertical";
  // 竖排时提示和菜单往右弹，横排（在底部）时往上弹
  const side = vertical ? "right" : "top";
  // 胶囊里分隔线的方向和胶囊排列方向相反
  const separator = vertical ? "horizontal" : "vertical";

  return (
    <>
      <ChromePill size="lg" orientation={orientation}>
        <DropdownMenu modal={false}>
          <ChromeTooltip label="添加节点" side={side}>
            <DropdownMenuTrigger
              render={<ChromeButton variant="primary" size="lg" aria-label="添加节点" />}
            >
              <Plus />
            </DropdownMenuTrigger>
          </ChromeTooltip>
          <DropdownMenuContent
            side={side}
            align="start"
            sideOffset={14}
            className={ADD_NODE_MENU_CONTENT_CLASS}
          >
            <AddNodeMenuBody
              items={addItems}
              onSelect={(value) => {
                if (value === UPLOAD_ACTION) onUpload();
                else if (value === ASSET_LIBRARY_ACTION) onOpenAssets();
                else onAdd(value as NodeKind);
              }}
            />
          </DropdownMenuContent>
        </DropdownMenu>

        <ChromeSeparator orientation={separator} />

        <ChromeSegment
          id="canvas-tool"
          size="lg"
          orientation={orientation}
          tooltipSide={side}
          value={tool}
          onValueChange={onToolChange}
          items={[
            { value: "select", label: "选择", shortcut: "V", icon: <MousePointer2 /> },
            { value: "pan", label: "抓手", shortcut: "H", icon: <Hand /> },
          ]}
        />

        <ChromeSeparator orientation={separator} />

        <ChromeTooltip label="撤销" shortcut={`${MOD}Z`} side={side}>
          <ChromeButton size="lg" aria-label="撤销" disabled={!canUndo} onClick={onUndo}>
            <Undo2 />
          </ChromeButton>
        </ChromeTooltip>
        <ChromeTooltip label="重做" shortcut={`⇧${MOD}Z`} side={side}>
          <ChromeButton size="lg" aria-label="重做" disabled={!canRedo} onClick={onRedo}>
            <Redo2 />
          </ChromeButton>
        </ChromeTooltip>

        <ChromeSeparator orientation={separator} className="max-sm:hidden" />

        <ChromeTooltip label="资产库" side={side}>
          <ChromeButton
            size="lg"
            aria-label="资产库"
            className="max-sm:hidden"
            onClick={onOpenAssets}
          >
            <FolderOpen />
          </ChromeButton>
        </ChromeTooltip>
        <ChromeTooltip label="任务历史" side={side}>
          <ChromeButton
            size="lg"
            aria-label="任务历史"
            className="max-sm:hidden"
            active={tasksOpen}
            onClick={() => setTasksOpen(true)}
          >
            <History />
          </ChromeButton>
        </ChromeTooltip>
      </ChromePill>

      <TasksSheet open={tasksOpen} onOpenChange={setTasksOpen} />
    </>
  );
}
