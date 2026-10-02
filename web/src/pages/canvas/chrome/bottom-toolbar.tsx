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
import { UPLOAD_ACTION } from "@/constants/canvas";
import type { NodeKind } from "@/types";

import { buildAddNodeItems } from "./add-node-items";
import { AssetsSheet } from "./assets-sheet";
import { MOD } from "./keys";
import { TasksSheet } from "./tasks-sheet";

/** 底部居中的主工具条：添加节点、选择 / 抓手、撤销重做、素材库、任务历史 */
export function BottomToolbar({
  tool,
  onToolChange,
  onAdd,
  onUpload,
  canUndo,
  canRedo,
  onUndo,
  onRedo,
}: {
  tool: CanvasTool;
  onToolChange: (tool: CanvasTool) => void;
  onAdd: (kind: NodeKind) => void;
  onUpload: () => void;
  canUndo: boolean;
  canRedo: boolean;
  onUndo: () => void;
  onRedo: () => void;
}) {
  const [sheet, setSheet] = useState<"assets" | "tasks" | null>(null);
  const addItems = useMemo(() => buildAddNodeItems(), []);

  return (
    <>
      <ChromePill size="lg">
        <DropdownMenu modal={false}>
          <ChromeTooltip label="添加节点">
            <DropdownMenuTrigger
              render={<ChromeButton variant="primary" size="lg" aria-label="添加节点" />}
            >
              <Plus />
            </DropdownMenuTrigger>
          </ChromeTooltip>
          <DropdownMenuContent
            side="top"
            align="start"
            sideOffset={14}
            className={ADD_NODE_MENU_CONTENT_CLASS}
          >
            <AddNodeMenuBody
              items={addItems}
              onSelect={(value) => {
                if (value === UPLOAD_ACTION) onUpload();
                else onAdd(value as NodeKind);
              }}
            />
          </DropdownMenuContent>
        </DropdownMenu>

        <ChromeSeparator />

        <ChromeSegment
          id="canvas-tool"
          size="lg"
          value={tool}
          onValueChange={onToolChange}
          items={[
            { value: "select", label: "选择", shortcut: "V", icon: <MousePointer2 /> },
            { value: "pan", label: "抓手", shortcut: "H", icon: <Hand /> },
          ]}
        />

        <ChromeSeparator />

        <ChromeTooltip label="撤销" shortcut={`${MOD}Z`}>
          <ChromeButton size="lg" aria-label="撤销" disabled={!canUndo} onClick={onUndo}>
            <Undo2 />
          </ChromeButton>
        </ChromeTooltip>
        <ChromeTooltip label="重做" shortcut={`⇧${MOD}Z`}>
          <ChromeButton size="lg" aria-label="重做" disabled={!canRedo} onClick={onRedo}>
            <Redo2 />
          </ChromeButton>
        </ChromeTooltip>

        <ChromeSeparator className="max-sm:hidden" />

        <ChromeTooltip label="画布素材">
          <ChromeButton
            size="lg"
            aria-label="画布素材"
            className="max-sm:hidden"
            active={sheet === "assets"}
            onClick={() => setSheet("assets")}
          >
            <FolderOpen />
          </ChromeButton>
        </ChromeTooltip>
        <ChromeTooltip label="任务历史">
          <ChromeButton
            size="lg"
            aria-label="任务历史"
            className="max-sm:hidden"
            active={sheet === "tasks"}
            onClick={() => setSheet("tasks")}
          >
            <History />
          </ChromeButton>
        </ChromeTooltip>
      </ChromePill>

      <AssetsSheet
        open={sheet === "assets"}
        onOpenChange={(open) => setSheet(open ? "assets" : null)}
      />
      <TasksSheet
        open={sheet === "tasks"}
        onOpenChange={(open) => setSheet(open ? "tasks" : null)}
      />
    </>
  );
}
