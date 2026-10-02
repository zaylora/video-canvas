import type { ComponentType, SVGProps } from "react";
import { Link, useLocation } from "react-router";
import { motion } from "motion/react";
import {
  CircleUser,
  House,
  Loader2,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Settings2,
  Trophy,
  Tv,
  Workflow,
} from "lucide-react";

import { Kbd } from "@/components/canvas/chrome/chrome";
import { SoonTip } from "@/components/home/soon";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useCreateCanvas } from "@/hooks/use-canvas-list";
import { SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { canManageModels } from "@/utils/admin/role";

/** 侧栏里的一项导航 */
type NavItem = {
  /** 显示名，收起后也是 Tooltip 的文字 */
  label: string;
  icon: ComponentType<SVGProps<SVGSVGElement>>;
  /** 跳转地址；没有就是还没上线的入口 */
  to?: string;
};

const NAV: NavItem[] = [
  { label: "首页", icon: House, to: "/" },
  { label: "无限画布", icon: Workflow, to: "/canvases" },
  { label: "作品广场", icon: Tv },
  { label: "创作活动", icon: Trophy },
];

/**
 * 菜单按钮统一高度 40px，收起后是 40px 见方的图标按钮。
 * overflow-visible：选中块用 layoutId 在项之间滑动，被按钮自带的 overflow-hidden 裁掉就成了“跳”过去
 */
const ITEM_CLASS =
  "h-10 gap-3 overflow-visible rounded-[10px] px-2.5 text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-active:bg-transparent data-active:font-semibold data-active:text-foreground group-data-[collapsible=icon]:size-10! group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0! group-data-[collapsible=icon]:[&>span:last-child]:hidden [&_svg]:size-[18px]!";

/** 当前页的灰块 + 左侧竖条，用 layoutId 在菜单项之间滑动 */
function ActiveHighlight() {
  return (
    <motion.span
      layoutId="home-nav-active"
      transition={SPRING}
      className="bg-muted before:bg-foreground absolute inset-0 -z-10 rounded-[10px] before:absolute before:top-2.5 before:bottom-2.5 before:-left-2 before:w-[3px] before:rounded-r-full"
    />
  );
}

/** 一项导航：能用的是链接，没上线的禁用并提示「即将上线」 */
function NavButton({ item, active }: { item: NavItem; active: boolean }) {
  const { setOpenMobile } = useSidebar();
  const content = (
    <>
      <item.icon />
      <span>{item.label}</span>
    </>
  );

  if (!item.to) {
    return (
      <SoonTip side="right" className="block cursor-not-allowed">
        <SidebarMenuButton disabled className={ITEM_CLASS}>
          {content}
        </SidebarMenuButton>
      </SoonTip>
    );
  }
  return (
    <SidebarMenuButton
      isActive={active}
      tooltip={item.label}
      className={cn(ITEM_CLASS, "relative isolate")}
      render={<Link to={item.to} onClick={() => setOpenMobile(false)} />}
    >
      {active && <ActiveHighlight />}
      {content}
    </SidebarMenuButton>
  );
}

/** 展开 / 收起按钮，Tooltip 里写快捷键 */
function CollapseButton() {
  const { state, toggleSidebar, isMobile } = useSidebar();
  if (isMobile) return null;
  const collapsed = state === "collapsed";
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <motion.button
            type="button"
            whileTap={TAP}
            onClick={toggleSidebar}
            aria-label={collapsed ? "展开侧栏" : "收起侧栏"}
            className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 grid size-8 shrink-0 place-items-center rounded-lg outline-none focus-visible:ring-3 [&_svg]:size-4"
          />
        }
      >
        {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
      </TooltipTrigger>
      <TooltipContent side="right" sideOffset={8}>
        {collapsed ? "展开侧栏" : "收起侧栏"}
        <Kbd>⌘B</Kbd>
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * 首页与所有画布页的左侧侧栏（设计稿 docs/design/首页 4.0）：
 * Logo、新建画布、导航，底部是 AI 配置（只给已确认的管理员）和我的账户。
 * 桌面端可收起成图标栏（⌘B），窄屏由 shadcn sidebar 换成从左滑出的抽屉。
 */
export function AppSidebar() {
  const { pathname } = useLocation();
  const { creating, create } = useCreateCanvas();
  /**
   * 和原来列表页一样，只有 store 里已确认是管理员时才显示后台入口；
   * 这里不主动探测角色，普通用户调 /admin/ai/me 会 403 并弹全局 toast。
   */
  const isAdmin = useAdminStore((state) => state.status === "ready" && canManageModels(state.role));

  return (
    <Sidebar collapsible="icon" className="border-sidebar-border">
      <SidebarHeader className="gap-3 px-2 pt-3">
        <div className="flex h-9 items-center justify-between gap-2 pl-1.5 group-data-[collapsible=icon]:h-auto group-data-[collapsible=icon]:gap-3 group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:pl-0">
          <Link
            to="/"
            className="flex min-w-0 items-center gap-2 rounded-md text-[15px] font-semibold outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <img src="/favicon.svg" alt="" className="size-7 shrink-0 rounded-lg" />
            <span className="truncate group-data-[collapsible=icon]:hidden">Video Canvas</span>
          </Link>
          <CollapseButton />
        </div>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip="新建画布"
              disabled={creating}
              onClick={() => void create()}
              className="bg-primary text-primary-foreground hover:bg-primary/85 hover:text-primary-foreground active:bg-primary/85 active:text-primary-foreground h-10 justify-center gap-2 rounded-xl font-semibold group-data-[collapsible=icon]:size-10! group-data-[collapsible=icon]:p-0! [&_svg]:size-[18px]!"
            >
              {creating ? <Loader2 className="animate-spin" /> : <Plus />}
              <span className="group-data-[collapsible=icon]:hidden">
                {creating ? "正在创建…" : "新建画布"}
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup className="px-2">
          <SidebarMenu className="gap-0.5">
            {NAV.map((item) => (
              <SidebarMenuItem key={item.label}>
                <NavButton item={item} active={item.to === pathname} />
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="px-2 pb-3">
        <SidebarMenu className="gap-0.5">
          {isAdmin && (
            <SidebarMenuItem>
              <SidebarMenuButton
                tooltip="AI 配置"
                className={ITEM_CLASS}
                render={<Link to="/admin/ai" />}
              >
                <Settings2 />
                <span>AI 配置</span>
              </SidebarMenuButton>
              <SidebarMenuBadge className="bg-muted text-muted-foreground top-2.5! right-2 font-normal">
                管理员
              </SidebarMenuBadge>
            </SidebarMenuItem>
          )}
          <SidebarMenuItem>
            <NavButton item={{ label: "我的账户", icon: CircleUser }} active={false} />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  );
}
