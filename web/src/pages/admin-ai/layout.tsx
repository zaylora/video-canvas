import { useEffect, type CSSProperties } from "react";
import { Shield } from "lucide-react";
import { Outlet, useLocation } from "react-router";

import { Button } from "@/components/ui/button";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { AdminHeader } from "@/components/admin-ui/admin-header";
import { ThemeSwitch } from "@/components/admin-ui/theme-switch";
import { useAdminStore } from "@/store/admin";
import { useSettingsStore } from "@/store/settings";
import { ROLE_LABEL } from "@/utils/admin/role";

import { AdminSidebar } from "./admin-sidebar";
import { findNav } from "./admin-nav";
import { Tag } from "@/components/admin-ui/tag";
import { ForbiddenView } from "./shared";
import { useAdminCatalog, type AdminOutletContext } from "./use-admin";

export default function AdminAiLayout() {
  const role = useAdminStore((state) => state.role);
  const status = useAdminStore((state) => state.status);
  const load = useAdminStore((state) => state.load);
  const { pathname } = useLocation();
  const theme = useSettingsStore((state) => state.theme);
  const updateSettings = useSettingsStore((state) => state.updateSettings);

  useEffect(() => {
    void load();
  }, [load]);

  // 后台用设计稿的 zinc 配色：挂在 <html> 上，弹窗（挂在 body）也能拿到；离开后台时摘掉
  useEffect(() => {
    document.documentElement.classList.add("admin-theme");
    return () => document.documentElement.classList.remove("admin-theme");
  }, []);

  const catalog = useAdminCatalog(status === "ready");

  if (status === "forbidden") return <ForbiddenView onRetry={() => void load(true)} />;

  if (status === "error") {
    return (
      <main className="grid min-h-svh place-items-center p-6">
        <div className="flex max-w-sm flex-col items-center gap-3 text-center">
          <h1 className="text-lg font-semibold">没能确认你的管理权限</h1>
          <p className="text-muted-foreground text-sm">网络或服务暂时有问题，不代表你没有权限。</p>
          <Button variant="outline" onClick={() => void load(true)}>
            重试
          </Button>
        </div>
      </main>
    );
  }

  if (status !== "ready" || !role) {
    return (
      <div className="flex h-svh" aria-busy="true" aria-label="正在确认管理权限">
        <Skeleton className="hidden h-full w-64 rounded-none lg:block" />
        <div className="flex flex-1 flex-col">
          <div className="flex h-14 shrink-0 items-center gap-3 border-b px-4">
            <Skeleton className="h-5 w-40" />
          </div>
          <div className="grid flex-1 gap-4 p-6 md:grid-cols-[16rem_1fr]">
            <Skeleton className="h-64" />
            <Skeleton className="h-96" />
          </div>
        </div>
      </div>
    );
  }

  const context: AdminOutletContext = { catalog };
  const counts = {
    "ai/channels": catalog.channelsStatus === "ready" ? catalog.channels.length : undefined,
    "ai/plugins": catalog.pluginsStatus === "ready" ? catalog.plugins.length : undefined,
  };
  const current = findNav(pathname);

  const dark =
    theme === "dark" ||
    (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  return (
    <SidebarProvider style={{ "--sidebar-width": "15rem" } as CSSProperties}>
      <AdminSidebar role={role} counts={counts} />
      <SidebarInset className="h-svh min-w-0 overflow-hidden">
        <AdminHeader>
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem className="hidden sm:inline-flex">
                {current?.group.label ?? "后台管理"}
              </BreadcrumbItem>
              <BreadcrumbSeparator className="hidden sm:block" />
              <BreadcrumbItem>
                <BreadcrumbPage>{current?.item.label ?? "后台"}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
          <div className="ml-auto flex items-center gap-2">
            <Tag
              tone={role === "super_admin" ? "info" : "neutral"}
              title={ROLE_LABEL[role]}
              className="hidden sm:inline-flex"
            >
              <Shield />
              {role === "super_admin" ? "超级管理员" : "管理员"}
            </Tag>
            <ThemeSwitch
              dark={dark}
              onToggle={() => updateSettings("theme", dark ? "light" : "dark")}
            />
          </div>
        </AdminHeader>
        <div className="min-h-0 flex-1">
          <Outlet context={context} />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
