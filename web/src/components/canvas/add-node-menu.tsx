import { Fragment, type ReactNode } from "react";
import type { XYPosition } from "@xyflow/react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

/** 菜单项：value 是回调里原样带回的标识，菜单本身不关心它的含义 */
export type AddNodeMenuItem = {
  value: string;
  label: string;
  icon?: ReactNode;
  disabled?: boolean;
  /** 在这项上面画条分隔线，用来隔开不同性质的几组 */
  separated?: boolean;
};

type AddNodeMenuProps = {
  /** 菜单锚点的屏幕坐标，null 表示关闭 */
  position: XYPosition | null;
  /** 菜单标题 */
  label?: string;
  /** 菜单项清单，包括哪些不可点，都由调用方算好 */
  items: AddNodeMenuItem[];
  onClose: () => void;
  onSelect: (value: string) => void;
};

/** 跟随鼠标位置弹出的「添加节点」菜单。 */
export function AddNodeMenu({
  position,
  label = "添加节点",
  items,
  onClose,
  onSelect,
}: AddNodeMenuProps) {
  return (
    <DropdownMenu
      open={position !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      modal={false}
    >
      {/* 0 尺寸的锚点，跟随双击位置，菜单据此定位 */}
      <DropdownMenuTrigger
        tabIndex={-1}
        aria-label="画布菜单"
        className="pointer-events-none fixed size-0 opacity-0"
        style={{ left: position?.x ?? 0, top: position?.y ?? 0 }}
      />
      <DropdownMenuContent className="w-52 p-2" align="start" sideOffset={2}>
        <DropdownMenuGroup>
          {/* GroupLabel 必须放在 Group 内，否则 Base UI 会抛 MenuGroupContext 缺失 */}
          <DropdownMenuLabel>{label}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {items.map((item) => (
            <Fragment key={item.value}>
              {item.separated && <DropdownMenuSeparator />}
              <DropdownMenuItem
                className="my-1 min-h-11 gap-3 px-3"
                disabled={item.disabled}
                onClick={() => onSelect(item.value)}
              >
                {item.icon}
                {item.label}
              </DropdownMenuItem>
            </Fragment>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
