import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { LogOut, Monitor, Moon, ShieldCheck, Sun } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarTrigger } from "@/components/ui/sidebar";
import type { ThemeSetting } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { CreditsPill, useUsername } from "@/pages/canvas/chrome/top-right-bar";
import { useSettingsStore } from "@/store";
import { removeToken } from "@/utils/storage/token";

const THEMES: { value: ThemeSetting; label: string; icon: typeof Sun }[] = [
  { value: "system", label: "跟随系统", icon: Monitor },
  { value: "dark", label: "深色", icon: Moon },
  { value: "light", label: "浅色", icon: Sun },
];

/** 头像菜单：主题、管理后台、退出登录 */
function AccountMenu() {
  const navigate = useNavigate();
  const username = useUsername();
  const theme = useSettingsStore((state) => state.theme);
  const updateSettings = useSettingsStore((state) => state.updateSettings);

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        aria-label="账户"
        className={cn(
          "bg-chrome ring-chrome-border grid size-9 place-items-center rounded-full text-sm font-semibold uppercase ring-1",
          "hover:ring-node-ring/40 focus-visible:ring-node-ring/60 transition-shadow outline-none focus-visible:ring-2",
        )}
      >
        {username?.slice(0, 1) ?? "我"}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={8} className="w-52">
        {username && (
          <DropdownMenuGroup>
            <DropdownMenuLabel className="truncate">{username}</DropdownMenuLabel>
          </DropdownMenuGroup>
        )}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            {theme === "light" ? <Sun /> : theme === "dark" ? <Moon /> : <Monitor />}
            主题
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-40">
            <DropdownMenuRadioGroup
              value={theme}
              onValueChange={(value) => updateSettings("theme", value as ThemeSetting)}
            >
              {THEMES.map((item) => (
                <DropdownMenuRadioItem key={item.value} value={item.value}>
                  <item.icon />
                  {item.label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuItem onClick={() => navigate("/admin/ai")}>
          <ShieldCheck />
          管理后台
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onClick={() => {
            removeToken();
            navigate("/login", { replace: true });
          }}
        >
          <LogOut />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * 首页与所有画布页的顶栏：吸顶、半透明，滚动后多一条底边。
 * 左边的汉堡按钮只在窄屏出现（桌面端侧栏自己有收起按钮），右边是积分和头像。
 */
export function SiteHeader() {
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 4);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header
      data-slot="site-header"
      className={cn(
        "bg-background/80 sticky top-0 z-20 flex h-14 shrink-0 items-center gap-2 border-b border-transparent px-4 backdrop-blur-xl transition-colors duration-150 md:px-6",
        scrolled && "border-border",
      )}
    >
      <SidebarTrigger className="md:hidden" aria-label="打开菜单" />
      <div className="flex-1" />
      <CreditsPill />
      <AccountMenu />
    </header>
  );
}
