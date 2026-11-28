import { Plus } from "lucide-react";

import { ADD_NODE_MENU_CONTENT_CLASS, AddNodeMenuBody } from "@/components/canvas/add-node-menu";
import { ChromeButton, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ASSET_LIBRARY_ACTION, UPLOAD_ACTION } from "@/constants/canvas";
import type { NodeKind } from "@/types";

import { buildAddNodeItems } from "./add-node-items";

const items = buildAddNodeItems();

export function AddNodeButton({
  side,
  size = "lg",
  onAdd,
  onUpload,
  onOpenAssets,
}: {
  side: "right" | "top" | "bottom";
  size?: "default" | "lg";
  onAdd: (kind: NodeKind) => void;
  onUpload: () => void;
  onOpenAssets: () => void;
}) {
  return (
    <DropdownMenu modal={false}>
      <ChromeTooltip label="添加节点" side={side}>
        <DropdownMenuTrigger
          render={
            <ChromeButton
              variant="primary"
              size={size}
              className={size === "default" ? "w-8 [&_svg]:size-4" : undefined}
              aria-label="添加节点"
            />
          }
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
          items={items}
          onSelect={(value) => {
            if (value === UPLOAD_ACTION) onUpload();
            else if (value === ASSET_LIBRARY_ACTION) onOpenAssets();
            else onAdd(value as NodeKind);
          }}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
