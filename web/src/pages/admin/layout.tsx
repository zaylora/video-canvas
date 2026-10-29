import { useCallback, useEffect, useLayoutEffect, useState, type CSSProperties } from "react";
import { ShieldCheck } from "lucide-react";
import { Outlet, useLocation } from "react-router";

import { FocusLoader } from "@/components/focus-loader";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { ChromePill } from "@/components/canvas/chrome/chrome";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminStore } from "@/store/admin";

import { AdminSidebar } from "./admin-sidebar";
import { usePageTitle } from "@/hooks/use-page-title";
import { adminPageTitle } from "@/utils/page-title";

import { findNav } from "./admin-nav";
import { ForbiddenView } from "./shared";

/** 入场动画播多久后摘掉 data-entering，和画布页一致 */
const ENTERING_MS = 1200;

/**
 * 后台外壳（设计稿 docs/design/首页 第 10 节，方案 A「聚焦显影」）：
 * 进后台和进画布一样先出全屏加载层，管理权限就在这一层里确认，
 * 角色有结果（就绪、无权限或失败）且 logo 聚焦完后加载层退场，侧栏、顶栏、正文依次入场。
 */
export default function AdminLayout() {
  const status = useAdminStore((state) => state.status);
  const [revealed, setRevealed] = useState(false);
  /** 角色已经缓存过就不说“确认权限”了 */
  const [cached] = useState(() => status === "ready");

  /**
   * 后台用设计稿的 zinc 配色：挂在 <html> 上，弹窗（挂在 body）也能拿到；离开后台时摘掉。
   * 用 layout effect：在首帧绘制前挂上，加载层第一眼就是后台配色，不会先闪一下首页的灰
   */
  useLayoutEffect(() => {
    document.documentElement.classList.add("admin-theme");
    return () => document.documentElement.classList.remove("admin-theme");
  }, []);

  /** 入场直接挂在根节点上，不走 state：免得退场刚开始时整个后台重渲染一遍 */
  const onOpen = useCallback(() => {
    const root = document.querySelector<HTMLElement>("[data-admin-root]");
    if (!root) return;
    root.dataset.entering = "";
    window.setTimeout(() => delete root.dataset.entering, ENTERING_MS);
  }, []);
  const onDone = useCallback(() => setRevealed(true), []);

  return (
    <>
      <AdminBody />
      {!revealed && (
        <FocusLoader
          icon={<ShieldCheck className="size-9" strokeWidth={1.8} aria-hidden />}
          title="AI 配置管理"
          subtitle={cached ? "正在进入后台" : "正在确认管理权限"}
          label="正在进入后台"
          ready={status === "ready" || status === "forbidden" || status === "error"}
          onOpen={onOpen}
          onDone={onDone}
        />
      )}
    </>
  );
}

/** 后台的实际内容：按角色状态显示无权限、失败、骨架或正文 */
function AdminBody() {
  const role = useAdminStore((state) => state.role);
  const status = useAdminStore((state) => state.status);
  const load = useAdminStore((state) => state.load);
  const { pathname } = useLocation();
  usePageTitle(adminPageTitle(pathname));

  useEffect(() => {
    void load();
  }, [load]);

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

  const current = findNav(pathname);

  return (
    <SidebarProvider
      data-admin-root
      style={{ "--sidebar-width": "15rem", "--sidebar-width-icon": "3rem" } as CSSProperties}
    >
      <AdminSidebar />
      {/* 和首页同一套外壳：侧栏是底层，内容区是浮起的圆角页面；内容在区内滚动，高度扣掉上下外边距 */}
      <SidebarInset className="ring-border h-svh min-w-0 overflow-hidden md:peer-data-[variant=inset]:h-[calc(100svh-1rem)] md:peer-data-[variant=inset]:rounded-[22px] md:peer-data-[variant=inset]:shadow-[0_12px_40px_-18px_oklch(0_0_0/0.35)] md:peer-data-[variant=inset]:ring-1">
        <header className="bg-background/85 sticky top-0 z-20 flex h-14 shrink-0 items-center gap-3 px-4 backdrop-blur lg:px-5">
          <SidebarTrigger className="size-8 rounded-full" />
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
          <ChromePill className="ml-auto">
            <ThemeToggle />
          </ChromePill>
        </header>
        <div data-slot="admin-outlet" className="min-h-0 flex-1">
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
