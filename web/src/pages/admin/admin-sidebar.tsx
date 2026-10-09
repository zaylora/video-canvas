import { ArrowLeft, User } from "lucide-react";
import { Link, useLocation } from "react-router";

import type { AdminRole } from "@/api/admin/ai/type.d";
import { NavUser } from "@/components/admin-ui/nav-user";
import {
  NAV_ITEM_CLASS,
  NAV_LINK_CLASS,
  NavActiveHighlight,
  SidebarBrand,
} from "@/components/home/sidebar-nav";
import { DropdownMenuItem, DropdownMenuShortcut } from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";

import { ADMIN_NAV } from "./admin-nav";

const ROLE_TEXT: Record<AdminRole, string> = {
  super_admin: "运维",
  admin: "运营",
};
const ROLE_DESC: Record<AdminRole, string> = {
  super_admin: "超级管理员",
  admin: "管理员",
};

export function AdminSidebar({
  role,
  counts,
}: {
  role: AdminRole;
  counts: Record<string, number | undefined>;
}) {
  const { pathname } = useLocation();
  const { setOpenMobile } = useSidebar();

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="gap-3 px-2 pt-3">
        <div className="flex h-9 items-center pl-1.5 group-data-[collapsible=icon]:h-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:pl-0">
          <SidebarBrand to="/admin/ai/overview" />
        </div>
      </SidebarHeader>
      <SidebarContent>
        {ADMIN_NAV.map((section) => (
          <SidebarGroup key={section.label} className="px-2">
            <SidebarGroupLabel className="text-sidebar-foreground/55 px-2.5">
              {section.label}
            </SidebarGroupLabel>
            <SidebarMenu className="gap-0.5">
              {section.items.map((item) => {
                const url = `/admin/${item.to}`;
                const badge = item.badge ?? counts[item.to];
                const active = pathname === url || pathname.startsWith(`${url}/`);
                return (
                  <SidebarMenuItem key={url}>
                    <SidebarMenuButton
                      isActive={active}
                      tooltip={item.label}
                      className={NAV_LINK_CLASS}
                      render={<Link to={url} onClick={() => setOpenMobile(false)} />}
                    >
                      {active && <NavActiveHighlight layoutId="admin-nav-active" />}
                      <item.icon />
                      <span className="group-data-[collapsible=icon]:hidden">{item.label}</span>
                      {badge !== undefined && (
                        <span className="text-sidebar-foreground/55 ml-auto font-mono text-xs group-data-[collapsible=icon]:hidden">
                          {badge}
                        </span>
                      )}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter className="border-sidebar-border border-t px-2 pb-3">
        {/* 离开后台是导航，不是账号操作，放在底部账号区的上面 */}
        <SidebarMenu className="gap-0.5">
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip="返回画布"
              className={NAV_ITEM_CLASS}
              render={<Link to="/" onClick={() => setOpenMobile(false)} />}
            >
              <ArrowLeft />
              <span>返回画布</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <NavUser
          name={ROLE_TEXT[role]}
          description={ROLE_DESC[role]}
          initials={role === "super_admin" ? "运" : "营"}
        >
          {/* 个人中心页还没有，先禁用 */}
          <DropdownMenuItem disabled>
            <User />
            个人中心
            <DropdownMenuShortcut>即将上线</DropdownMenuShortcut>
          </DropdownMenuItem>
        </NavUser>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
