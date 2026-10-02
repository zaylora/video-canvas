import { ArrowLeft, Clapperboard } from "lucide-react";
import { Link, useNavigate } from "react-router";

import type { AdminRole } from "@/api/admin-ai/type";
import { NavBrand } from "@/components/admin-ui/nav-brand";
import { NavGroup } from "@/components/admin-ui/nav-group";
import { NavUser } from "@/components/admin-ui/nav-user";
import { DropdownMenuGroup, DropdownMenuItem } from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
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
  const navigate = useNavigate();

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <NavBrand
          name="Video Canvas"
          subtitle="AI 配置管理"
          icon={Clapperboard}
          render={<Link to="/admin/ai/overview" />}
        />
      </SidebarHeader>
      <SidebarContent>
        {ADMIN_NAV.map((section) => (
          <NavGroup
            key={section.label}
            title={section.label}
            items={section.items.map((item) => ({
              title: item.label,
              url: `/admin/${item.to}`,
              icon: item.icon,
              badge: counts[item.to],
            }))}
          />
        ))}
      </SidebarContent>
      <SidebarFooter className="border-sidebar-border border-t">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip="返回画布"
              className="text-sidebar-foreground/80"
              render={<Link to="/" />}
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
          <DropdownMenuGroup>
            <DropdownMenuItem onClick={() => navigate("/")}>
              <ArrowLeft />
              返回画布
            </DropdownMenuItem>
          </DropdownMenuGroup>
        </NavUser>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
