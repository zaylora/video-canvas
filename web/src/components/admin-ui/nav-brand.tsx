import type { ElementType, ReactElement } from "react";

import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";

/** 侧栏顶部的品牌（shadcn-admin 的 AppTitle / TeamSwitcher 外观）；render 传链接元素 */
function NavBrand({
  name,
  subtitle,
  icon: Icon,
  render,
}: {
  name: string;
  subtitle?: string;
  icon: ElementType;
  /** 外层元素，例如 <Link to="/" /> */
  render?: ReactElement;
}) {
  return (
    <SidebarMenu data-slot="nav-brand">
      <SidebarMenuItem>
        <SidebarMenuButton size="lg" render={render}>
          <div className="bg-primary text-primary-foreground flex aspect-square size-8 items-center justify-center rounded-lg">
            <Icon className="size-4" />
          </div>
          <div className="grid flex-1 text-left text-sm leading-tight">
            <span className="truncate font-semibold">{name}</span>
            {subtitle && (
              <span className="text-sidebar-foreground/60 truncate text-xs">{subtitle}</span>
            )}
          </div>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

export { NavBrand };
