import type { ElementType } from "react";
import { Link, useLocation } from "react-router";

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";

type NavItem = {
  title: string;
  /** 完整路径；当前路径等于它或在它下面时高亮 */
  url: string;
  icon?: ElementType;
  /** 右侧徽标；undefined 不显示 */
  badge?: string | number;
};

type NavGroupData = {
  title: string;
  items: NavItem[];
};

const isActive = (pathname: string, url: string) =>
  pathname === url || pathname.startsWith(`${url}/`);

/** 侧栏的一组导航（shadcn-admin 的 NavGroup）：组标题 + 链接项，收起时显示提示 */
function NavGroup({ title, items }: NavGroupData) {
  const { pathname } = useLocation();
  const { setOpenMobile } = useSidebar();
  return (
    <SidebarGroup data-slot="nav-group">
      <SidebarGroupLabel className="text-sidebar-foreground/55">{title}</SidebarGroupLabel>
      <SidebarMenu>
        {items.map((item) => (
          <SidebarMenuItem key={item.url}>
            <SidebarMenuButton
              isActive={isActive(pathname, item.url)}
              tooltip={item.title}
              render={<Link to={item.url} onClick={() => setOpenMobile(false)} />}
            >
              {item.icon && <item.icon />}
              <span>{item.title}</span>
              {item.badge !== undefined && (
                <span className="text-sidebar-foreground/55 ml-auto font-mono text-xs group-data-[collapsible=icon]:hidden">
                  {item.badge}
                </span>
              )}
            </SidebarMenuButton>
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </SidebarGroup>
  );
}

export { NavGroup, type NavGroupData, type NavItem };
