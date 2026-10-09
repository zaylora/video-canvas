import type { ComponentType, SVGProps } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { Clapperboard, Compass, FolderOpen, ShieldCheck, Workflow } from "lucide-react";

import { CanvasCover } from "@/components/home/canvas-cover";
import { ConversationList } from "@/components/home/conversation-list";
import { ListItem, ListSection } from "@/components/home/sidebar-list";
import {
  NAV_ITEM_CLASS,
  NAV_LINK_CLASS,
  NavActiveHighlight,
  SidebarBrand,
} from "@/components/home/sidebar-nav";
import { SidebarAccount } from "@/components/home/sidebar-account";
import { SoonTip } from "@/components/home/soon";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
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
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";
import { useRecentCanvases } from "@/hooks/use-recent-canvases";
import { useCanEnterAdmin } from "@/pages/canvas/chrome/top-right-bar";

/** 侧栏里的一项导航 */
type NavItem = {
  /** 显示名，收起后也是 Tooltip 的文字 */
  label: string;
  icon: ComponentType<SVGProps<SVGSVGElement>>;
  /** 跳转地址；没有就是还没上线的入口 */
  to?: string;
};

const NAV: NavItem[] = [
  { label: "创作", icon: Clapperboard, to: "/" },
  { label: "探索", icon: Compass },
  { label: "资产", icon: FolderOpen, to: "/assets" },
];

/** 每个分组最多列几项 */
const LIST_LIMIT = 6;

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
        <SidebarMenuButton disabled className={NAV_ITEM_CLASS}>
          {content}
        </SidebarMenuButton>
      </SoonTip>
    );
  }
  return (
    <SidebarMenuButton
      isActive={active}
      tooltip={item.label}
      className={NAV_LINK_CLASS}
      render={<Link to={item.to} onClick={() => setOpenMobile(false)} />}
    >
      {active && <NavActiveHighlight layoutId="home-nav-active" />}
      {content}
    </SidebarMenuButton>
  );
}

/**
 * 首页、资产、对话和画布列表共用的左侧侧栏（设计稿 docs/品牌包装/登录与首页改版原型）：
 * 连镜 Logo、导航（创作 / 探索 / 资产）、「对话」和「画布」两组最近记录，
 * 底部是管理后台入口（只给已确认的管理员）和账号菜单（退出登录）。
 * 对话和画布都是真实数据：对话来自 store/conversations，画布是最近画布。
 * 桌面端可收起成图标栏（⌘B），和后台一样：收起按钮在顶栏左侧（SiteHeader），侧栏右边缘还有一条可点的 Rail；窄屏由 shadcn sidebar 换成从左滑出的抽屉。
 */
export function AppSidebar() {
  const { pathname } = useLocation();
  const canvases = useRecentCanvases(LIST_LIMIT);
  /**
   * 管理后台入口、管理员徽标都读登录时存在本机的角色：刷新页面后 store/admin 回到未加载，
   * 读它会让入口凭空消失；这里也不主动请求角色，普通用户调 /admin/ai/me 会 403 并弹全局 toast。
   */
  const isAdmin = useCanEnterAdmin();
  const navigate = useNavigate();

  return (
    <Sidebar collapsible="icon" className="border-sidebar-border">
      <SidebarHeader className="gap-3 px-2 pt-3">
        <div className="flex h-9 items-center pl-1.5 group-data-[collapsible=icon]:h-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:pl-0">
          <SidebarBrand to="/" />
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
          <ConversationList pathname={pathname} />
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
                tooltip="管理后台"
                className={NAV_ITEM_CLASS}
                render={<Link to="/admin/ai" />}
              >
                <ShieldCheck />
                <span>管理后台</span>
              </SidebarMenuButton>
              <SidebarMenuBadge className="bg-muted text-muted-foreground top-2.5! right-2 font-normal">
                管理员
              </SidebarMenuBadge>
            </SidebarMenuItem>
          )}
        </SidebarMenu>
        <SidebarAccount>
          {isAdmin && (
            <DropdownMenuItem onClick={() => navigate("/admin/ai")}>
              <ShieldCheck />
              管理后台
            </DropdownMenuItem>
          )}
        </SidebarAccount>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
