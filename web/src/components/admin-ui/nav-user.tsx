import type { ReactNode } from "react";
import { ChevronsUpDown } from "lucide-react";

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";

/**
 * 侧栏底部的账号菜单（shadcn-admin 的 NavUser）：头像 + 名称 + 说明，点开是菜单。
 * children 是菜单内容（DropdownMenuGroup / DropdownMenuItem）。
 */
function NavUser({
  name,
  description,
  initials,
  children,
}: {
  name: string;
  description?: string;
  /** 头像里的文字 */
  initials: string;
  children?: ReactNode;
}) {
  const { isMobile } = useSidebar();
  const identity = (
    <>
      <Avatar className="size-8 rounded-lg">
        <AvatarFallback className="rounded-lg bg-gradient-to-br from-zinc-500 to-zinc-700 text-xs font-semibold text-white">
          {initials}
        </AvatarFallback>
      </Avatar>
      <div className="grid flex-1 text-left text-sm leading-tight">
        <span className="truncate font-medium">{name}</span>
        {description && (
          <span className="text-sidebar-foreground/60 truncate text-xs">{description}</span>
        )}
      </div>
    </>
  );

  return (
    <SidebarMenu data-slot="nav-user">
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                size="lg"
                className="data-[popup-open]:bg-sidebar-accent data-[popup-open]:text-sidebar-accent-foreground"
              />
            }
          >
            {identity}
            <ChevronsUpDown className="ml-auto size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="min-w-56 rounded-lg"
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
          >
            {/* Base UI 要求 Label 在 Group 里 */}
            <DropdownMenuGroup>
              <DropdownMenuLabel className="p-0 font-normal">
                <div className="flex items-center gap-2 px-1 py-1.5 text-sm">{identity}</div>
              </DropdownMenuLabel>
            </DropdownMenuGroup>
            {children && (
              <>
                <DropdownMenuSeparator />
                {children}
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

export { NavUser };
