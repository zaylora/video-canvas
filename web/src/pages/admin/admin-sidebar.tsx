import { useEffect } from "react";
import { ArrowLeft } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Link, useLocation } from "react-router";

import {
  NAV_ITEM_CLASS,
  NAV_LINK_CLASS,
  NavActiveHighlight,
  SidebarBrand,
} from "@/components/home/sidebar-nav";
import { SidebarAccount } from "@/components/home/sidebar-account";
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

import { DURATION, EASE_OUT } from "@/lib/motion";
import { useSystemUpdateStore } from "@/store/system-update";

import { ADMIN_NAV } from "./admin-nav";

/** 版本页的路径：Logo 旁的版本标记点了跳过去，导航条目有新版本时标「新」 */
const VERSION_PATH = "settings/version";

export function AdminSidebar() {
  const { pathname } = useLocation();
  const { setOpenMobile } = useSidebar();
  const check = useSystemUpdateStore((state) => state.check);
  const hasUpdate = !!check?.has_update;

  return (
    <Sidebar variant="inset" collapsible="icon">
      <SidebarHeader className="gap-3 px-2 pt-3 group-data-[collapsible=icon]:px-1">
        <div className="flex h-9 items-end gap-1.5 pb-1.5 pl-1.5 group-data-[collapsible=icon]:h-auto group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:pb-0 group-data-[collapsible=icon]:pl-0">
          <SidebarBrand to="/admin/ai/overview" />
          <VersionMarker onNavigate={() => setOpenMobile(false)} />
        </div>
      </SidebarHeader>
      <SidebarContent>
        {ADMIN_NAV.map((section) => (
          <SidebarGroup key={section.label} className="px-2 group-data-[collapsible=icon]:px-1">
            <SidebarGroupLabel className="text-sidebar-foreground/55 px-2.5">
              {section.label}
            </SidebarGroupLabel>
            <SidebarMenu className="gap-0.5">
              {section.items.map((item) => {
                const url = `/admin/${item.to}`;
                const badge = item.to === VERSION_PATH && hasUpdate ? "新" : item.badge;
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
      <SidebarFooter className="px-2 pb-3 group-data-[collapsible=icon]:px-1">
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
        <SidebarAccount />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

/**
 * Logo 右下方的小号版本标记：只显示当前版本，没有图标；有新版本时右侧亮一个小圆点，
 * 一键升级进行中换成转圈。侧栏收起后藏起（版本页仍可从导航进入）。
 * 进后台时检查一次新版本，结果和「版本与升级」页共用。
 * @param onNavigate 点击后关掉移动端的侧栏抽屉
 */
function VersionMarker({ onNavigate }: { onNavigate: () => void }) {
  const check = useSystemUpdateStore((state) => state.check);
  const upgrading = useSystemUpdateStore((state) => state.upgrading);
  const load = useSystemUpdateStore((state) => state.load);
  const reduceMotion = useReducedMotion();

  useEffect(() => {
    void load();
  }, [load]);

  if (!check) return null;
  const label = check.dev_build ? "dev" : `v${check.current}`;
  const hasUpdate = check.has_update && !!check.latest;
  const title = upgrading
    ? `正在升级，当前 ${label}`
    : hasUpdate
      ? `当前 ${label} · 有新版本 v${check.latest}`
      : `当前 ${label}`;
  return (
    <Link
      to={`/admin/${VERSION_PATH}`}
      onClick={onNavigate}
      title={title}
      className="text-sidebar-foreground/55 hover:text-sidebar-foreground focus-visible:ring-ring/50 flex shrink-0 items-center gap-1 rounded-sm font-mono text-[11px] leading-none tabular-nums outline-none focus-visible:ring-2 group-data-[collapsible=icon]:hidden"
    >
      {label}
      <AnimatePresence initial={false}>
        {upgrading ? (
          <span
            key="spin"
            aria-label="升级中"
            className="border-status-running size-2 animate-spin rounded-full border-[1.5px] border-t-transparent motion-reduce:animate-none"
          />
        ) : (
          hasUpdate && (
            <motion.span
              key="dot"
              aria-label="有新版本"
              className="size-1.5 rounded-full bg-yellow-500"
              initial={reduceMotion ? { opacity: 0 } : { opacity: 0, scale: 0.5 }}
              animate={{
                opacity: 1,
                scale: 1,
                transition: { duration: DURATION.base, ease: EASE_OUT },
              }}
              exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
            />
          )
        )}
      </AnimatePresence>
    </Link>
  );
}
