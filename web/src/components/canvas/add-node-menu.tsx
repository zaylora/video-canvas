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

/** 菜单面板的样式：双击菜单和左侧工具栏「添加」共用；实色灰底、无描边，照设计稿 6.14 */
export const ADD_NODE_MENU_CONTENT_CLASS = "w-50 rounded-lg bg-menu p-2 ring-0 shadow-xl";

/**
 * 「添加节点」菜单的内容（设计稿 6.14）：暗灰小标题，每项是图标 + 一行文字，
 * 不同性质的项之间用分隔线隔开。放进任意 DropdownMenuContent 里用。
 */
export function AddNodeMenuBody({
  label = "添加节点",
  items,
  onSelect,
}: {
  label?: string;
  items: AddNodeMenuItem[];
  onSelect: (value: string) => void;
}) {
  return (
    <DropdownMenuGroup>
      {/* GroupLabel 必须放在 Group 内，否则 Base UI 会抛 MenuGroupContext 缺失 */}
      <DropdownMenuLabel className="text-muted-foreground/65 px-2 py-1 text-xs leading-4.5 font-normal">
        {label}
      </DropdownMenuLabel>
      {items.map((item) => (
        <Fragment key={item.value}>
          {item.separated && <DropdownMenuSeparator className="bg-foreground/8 mx-2 my-1" />}
          <DropdownMenuItem
            className="focus:bg-chrome-hover h-10 gap-2 rounded-md px-2 text-sm"
            disabled={item.disabled}
            onClick={() => onSelect(item.value)}
          >
            <span className="flex shrink-0">{item.icon}</span>
            <span className="min-w-0 flex-1 truncate">{item.label}</span>
          </DropdownMenuItem>
        </Fragment>
      ))}
    </DropdownMenuGroup>
  );
}

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
export function AddNodeMenu({ position, label, items, onClose, onSelect }: AddNodeMenuProps) {
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
      <DropdownMenuContent className={ADD_NODE_MENU_CONTENT_CLASS} align="start" sideOffset={2}>
        <AddNodeMenuBody label={label} items={items} onSelect={onSelect} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
