import { ArrowLeft, User } from "lucide-react";
import { Link, useLocation } from "react-router";

import type { AdminRole } from "@/api/admin/ai/type.d";
import { Logo } from "@/components/brand/logo";
import { NavUser } from "@/components/admin-ui/nav-user";
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

const ROLE_TEXT: Record<AdminRole, string> = { super_admin: "运维", admin: "运营" };
const ROLE_DESC: Record<AdminRole, string> = { super_admin: "超级管理员", admin: "管理员" };

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
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" render={<Link to="/admin/ai/overview" />}>
              <Logo size={24} className="shrink-0" />
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">连镜</span>
                <span className="text-sidebar-foreground/60 truncate text-xs">AI 配置管理</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        {/* 离开后台是导航，不是账号操作，放在导航最上面，不放底部的账号区 */}
        <SidebarGroup className="pb-0">
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                tooltip="返回画布"
                className="text-sidebar-foreground/80"
                render={<Link to="/" onClick={() => setOpenMobile(false)} />}
              >
                <ArrowLeft />
                <span>返回画布</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroup>
        {ADMIN_NAV.map((section) => (
          <SidebarGroup key={section.label}>
            <SidebarGroupLabel className="text-sidebar-foreground/55">
              {section.label}
            </SidebarGroupLabel>
            <SidebarMenu>
              {section.items.map((item) => {
                const url = `/admin/${item.to}`;
                const badge = item.badge ?? counts[item.to];
                return (
                  <SidebarMenuItem key={url}>
                    <SidebarMenuButton
                      isActive={pathname === url || pathname.startsWith(`${url}/`)}
                      tooltip={item.label}
                      render={<Link to={url} onClick={() => setOpenMobile(false)} />}
                    >
                      <item.icon />
                      <span>{item.label}</span>
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
      <SidebarFooter className="border-sidebar-border border-t">
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
