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
import { cn } from "@/lib/utils";

/** 菜单项：value 是回调里原样带回的标识，菜单本身不关心它的含义 */
export type AddNodeMenuItem = {
  value: string;
  label: string;
  /** 标题下面一行灰字，说明这种节点干什么 */
  description?: string;
  /** 行尾的灰字提示 */
  hint?: string;
  icon?: ReactNode;
  disabled?: boolean;
  /** 在这项上面画条分隔线，用来隔开不同性质的几组 */
  separated?: boolean;
};

/** 菜单面板的样式：双击菜单和底部「添加」共用 */
export const ADD_NODE_MENU_CONTENT_CLASS = "w-72 rounded-2xl p-2";

/**
 * 「添加节点」菜单的内容（设计稿原型）：灰色小标题，每项是图标 + 标题 + 一行说明，
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
      <DropdownMenuLabel className="text-muted-foreground px-3 pt-2 pb-1 text-xs font-normal">
        {label}
      </DropdownMenuLabel>
      {items.map((item) => (
        <Fragment key={item.value}>
          {item.separated && <DropdownMenuSeparator className="mx-1 my-1.5" />}
          <DropdownMenuItem
            className={cn(
              "gap-3.5 rounded-xl px-3 [&_svg:not([class*='size-'])]:size-5",
              item.description ? "min-h-14 py-2" : "min-h-12",
            )}
            disabled={item.disabled}
            onClick={() => onSelect(item.value)}
          >
            <span className="text-muted-foreground flex shrink-0">{item.icon}</span>
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="text-[15px] leading-5">{item.label}</span>
              {item.description && (
                <span className="text-muted-foreground truncate text-xs">{item.description}</span>
              )}
            </span>
            {item.hint && (
              <span className="text-muted-foreground ml-auto shrink-0 text-xs">{item.hint}</span>
            )}
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
