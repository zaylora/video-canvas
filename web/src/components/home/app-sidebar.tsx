import { useMemo, type ComponentType, type ReactNode, type SVGProps } from "react";
import { Link, useLocation } from "react-router";
import { motion } from "motion/react";
import {
  CircleUser,
  Compass,
  FolderOpen,
  MessageSquare,
  PanelLeftClose,
  PanelLeftOpen,
  Pin,
  Settings2,
  Sparkles,
  Workflow,
} from "lucide-react";

import { Logo } from "@/components/brand/logo";
import { Kbd } from "@/components/canvas/chrome/chrome";
import { CanvasCover } from "@/components/home/canvas-cover";
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
import { buildSampleConversations } from "@/constants/conversation-sample";
import { useRecentCanvases } from "@/hooks/use-recent-canvases";
import { SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { canManageModels } from "@/utils/admin/role";
import { placeholderBackground } from "@/utils/home/placeholder";

/** 侧栏里的一项导航 */
type NavItem = {
  /** 显示名，收起后也是 Tooltip 的文字 */
  label: string;
  icon: ComponentType<SVGProps<SVGSVGElement>>;
  /** 跳转地址；没有就是还没上线的入口 */
  to?: string;
};

const NAV: NavItem[] = [
  { label: "创作", icon: Sparkles, to: "/" },
  { label: "探索", icon: Compass },
  { label: "资产", icon: FolderOpen, to: "/assets" },
];

/** 每个分组最多列几项 */
const LIST_LIMIT = 6;

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

/** 分组：一行小标题（右侧可放「全部」之类的入口）+ 下面的列表 */
function ListSection({
  title,
  action,
  children,
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="mt-2">
      <div className="flex h-8 items-center justify-between px-2.5">
        <p className="text-muted-foreground text-xs">{title}</p>
        {action}
      </div>
      <div className="grid gap-0.5">{children}</div>
    </section>
  );
}

/** 分组里的一项：24px 缩略图（封面或图标）+ 标题，当前项高亮 */
function ListItem({
  to,
  title,
  active,
  pinned,
  thumb,
}: {
  to: string;
  title: string;
  active: boolean;
  pinned?: boolean;
  thumb: ReactNode;
}) {
  const { setOpenMobile } = useSidebar();
  return (
    <Link
      to={to}
      title={title}
      aria-current={active ? "page" : undefined}
      onClick={() => setOpenMobile(false)}
      className={cn(
        "text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 flex h-9 items-center gap-2.5 rounded-[10px] pr-2 pl-2.5 text-sm outline-none focus-visible:ring-3",
        active && "bg-muted text-foreground font-medium",
      )}
    >
      <span className="bg-muted grid size-6 shrink-0 place-items-center overflow-hidden rounded-[7px] [&_svg]:size-3.5">
        {thumb}
      </span>
      <span className="min-w-0 flex-1 truncate">{title}</span>
      {pinned && <Pin aria-label="已置顶" className="size-3.5 shrink-0 opacity-55" />}
    </Link>
  );
}

/**
 * 首页、资产、对话和画布列表共用的左侧侧栏（设计稿 docs/品牌包装/登录与首页改版原型）：
 * 连镜 Logo、导航（创作 / 探索 / 资产）、「对话」和「画布」两组最近记录，
 * 底部是 AI 配置（只给已确认的管理员）和我的账户。
 * 对话目前是样例数据；画布是真的最近画布。
 * 桌面端可收起成图标栏（⌘B），窄屏由 shadcn sidebar 换成从左滑出的抽屉。
 */
export function AppSidebar() {
  const { pathname } = useLocation();
  const canvases = useRecentCanvases(LIST_LIMIT);
  const conversations = useMemo(() => buildSampleConversations(), []);
  /**
   * 和原来列表页一样，只有 store 里已确认是管理员时才显示后台入口；
   * 这里不主动探测角色，普通用户调 /admin/ai/me 会 403 并弹全局 toast。
   */
  const isAdmin = useAdminStore((state) => state.status === "ready" && canManageModels(state.role));

  return (
    <Sidebar collapsible="icon" className="border-sidebar-border">
      <SidebarHeader className="gap-3 px-2 pt-3">
        <div className="flex h-9 items-center justify-between gap-2 pl-1.5 group-data-[collapsible=icon]:h-auto group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:gap-3 group-data-[collapsible=icon]:pl-0">
          <Link
            to="/"
            aria-label="连镜"
            className="focus-visible:ring-ring/50 flex min-w-0 items-center gap-2 rounded-md text-[15px] font-semibold tracking-wide outline-none focus-visible:ring-3"
          >
            <Logo size={24} />
            <span className="truncate group-data-[collapsible=icon]:hidden">连镜</span>
          </Link>
          <CollapseButton />
        </div>
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

        {/* 收起成图标栏后放不下列表，整块隐藏 */}
        <div className="min-h-0 flex-1 overflow-y-auto px-2 group-data-[collapsible=icon]:hidden">
          <ListSection title="对话">
            {conversations.slice(0, LIST_LIMIT).map((conversation) => (
              <ListItem
                key={conversation.id}
                to={`/conversations/${conversation.id}`}
                title={conversation.title}
                active={pathname === `/conversations/${conversation.id}`}
                pinned={conversation.pinned}
                thumb={
                  conversation.hue === null ? (
                    <MessageSquare />
                  ) : (
                    <span
                      aria-hidden
                      className="size-full"
                      style={{ background: placeholderBackground(conversation.hue) }}
                    />
                  )
                }
              />
            ))}
          </ListSection>
          <ListSection
            title="画布"
            action={
              <Link
                to="/canvases"
                className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 rounded px-1 text-xs outline-none focus-visible:ring-2"
              >
                全部
              </Link>
            }
          >
            {canvases.map((canvas) => (
              <ListItem
                key={canvas.id}
                to={`/canvas/${canvas.id}`}
                title={canvas.title}
                active={false}
                thumb={
                  canvas.coverUrl ? (
                    <CanvasCover id={canvas.id} coverUrl={canvas.coverUrl} />
                  ) : (
                    <Workflow />
                  )
                }
              />
            ))}
          </ListSection>
        </div>
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
