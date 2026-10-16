import type { ReactNode } from "react";
import { MoreHorizontal } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

/**
 * 卡片右上角的「⋯」菜单：悬停或聚焦卡片时出现，菜单打开时保持可见；触屏设备常显。
 * 外层卡片需要带 `group/card` 类。菜单项由调用方放进 children。
 * @param label 触发按钮的读屏名字
 * @param children DropdownMenuItem 等菜单项
 */
export function CardMenu({
  label = "更多操作",
  children,
}: {
  label?: string;
  children: ReactNode;
}) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        aria-label={label}
        className={cn(
          "bg-cover-scrim text-cover-foreground absolute top-2 right-2 z-10 grid size-7 place-items-center rounded-full outline-none",
          "opacity-0 transition-opacity duration-150 group-hover/card:opacity-100 focus-visible:opacity-100 data-popup-open:opacity-100",
          "focus-visible:ring-cover-foreground/70 max-md:opacity-100 focus-visible:ring-2 [&_svg]:size-4",
        )}
      >
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={6} className="w-44">
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
