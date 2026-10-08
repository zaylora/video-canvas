import { useMemo } from "react";
import NumberFlow from "@number-flow/react";
import { useNavigate } from "react-router";
import {
  Keyboard,
  LogOut,
  Monitor,
  Moon,
  MoreHorizontal,
  Settings,
  ShieldCheck,
  Sparkle,
  Sun,
  User,
} from "lucide-react";

import { ChromeButton, ChromePill, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { UserAvatar } from "@/components/user-avatar";
import type { ThemeSetting } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { useSettingsStore } from "@/store";
import { useCreditsStore } from "@/store/credits";
import { useMeStore } from "@/store/me";
import { useWsStore, type ConnectionState } from "@/store/ws";
import { canEnterAdmin } from "@/utils/admin/role";
import { displayName } from "@/utils/profile/profile-rules";
import { logout } from "@/utils/storage/session";
import { getRole, getToken } from "@/utils/storage/token";

const THEMES: { value: ThemeSetting; label: string; icon: typeof Sun }[] = [
  { value: "system", label: "跟随系统", icon: Monitor },
  { value: "dark", label: "深色", icon: Moon },
  { value: "light", label: "浅色", icon: Sun },
];

const WS_TEXT: Record<ConnectionState, string> = {
  idle: "实时进度未连接",
  connecting: "正在连接实时进度",
  connected: "实时进度已连接",
  reconnecting: "实时进度已断开，正在重连",
  closed: "实时进度已断开",
};

/**
 * 从登录令牌里读出用户名，读不出就返回 null（令牌是不透明串时）。
 * 只在 /me 资料还没拉到时兜底，正常显示用 me.nickname || me.username
 */
export function useUsername() {
  return useMemo(() => {
    try {
      const payload = getToken()?.split(".")[1];
      if (!payload) return null;
      const json = JSON.parse(atob(payload.replace(/-/g, "+").replace(/_/g, "/")));
      const name = json.username ?? json.name ?? json.sub;
      return typeof name === "string" && name ? name : null;
    } catch {
      return null;
    }
  }, []);
}

/** 当前账号能否进入管理后台：读登录时存下的角色，普通用户不显示入口 */
export function useCanEnterAdmin() {
  return useMemo(() => canEnterAdmin(getRole()), []);
}

/** 积分胶囊：左边是实时连接状态点，点开看余额明细 */
export function CreditsPill() {
  const credits = useCreditsStore((state) => state.credits);
  const refresh = useCreditsStore((state) => state.refresh);
  const connection = useWsStore((state) => state.connection);
  const down = connection === "reconnecting" || connection === "closed";
  const pending = connection === "connecting" || connection === "idle";

  return (
    <Popover onOpenChange={(open) => open && void refresh()}>
      <ChromePill>
        <PopoverTrigger
          render={
            <ChromeButton
              aria-label={`积分，${WS_TEXT[connection]}`}
              className="text-foreground gap-2 pr-3 pl-2.5 font-mono text-sm font-semibold"
            >
              <span
                title={WS_TEXT[connection]}
                className={cn(
                  "size-1.5 rounded-full",
                  down
                    ? "bg-status-warning ring-status-warning/20 animate-pulse ring-3"
                    : pending
                      ? "bg-muted-foreground/50"
                      : "bg-status-success ring-status-success/15 ring-3",
                )}
              />
              <Sparkle className="fill-credit text-credit size-4!" strokeWidth={0} />
              {credits ? (
                <NumberFlow value={credits.available} className="tabular-nums" />
              ) : (
                <span className="text-muted-foreground">—</span>
              )}
            </ChromeButton>
          }
        />
      </ChromePill>
      <PopoverContent align="end" sideOffset={10} className="w-64 gap-0 rounded-xl p-0">
        <div className="flex items-baseline gap-2 px-4 pt-4 pb-3">
          <span className="font-mono text-2xl font-semibold tabular-nums">
            {credits ? <NumberFlow value={credits.available} /> : "—"}
          </span>
          <span className="text-muted-foreground text-xs">可用积分</span>
        </div>
        <dl className="border-border grid grid-cols-[1fr_auto] gap-y-2 border-t px-4 py-3 text-xs">
          <dt className="text-muted-foreground">总余额</dt>
          <dd className="font-mono tabular-nums">{credits?.balance ?? "—"}</dd>
          <dt className="text-muted-foreground">生成中冻结</dt>
          <dd className="font-mono tabular-nums">{credits?.frozen ?? "—"}</dd>
        </dl>
        <p
          className={cn(
            "border-border flex items-center gap-2 border-t px-4 py-2.5 text-xs",
            down ? "text-status-warning" : "text-muted-foreground",
          )}
        >
          {WS_TEXT[connection]}
        </p>
      </PopoverContent>
    </Popover>
  );
}

/** 右上角：更多菜单、积分、账户 */
export function TopRightBar({
  onOpenSettings,
  onOpenShortcuts,
}: {
  onOpenSettings: () => void;
  onOpenShortcuts: () => void;
}) {
  const navigate = useNavigate();
  const theme = useSettingsStore((state) => state.theme);
  const updateSettings = useSettingsStore((state) => state.updateSettings);
  const tokenUsername = useUsername();
  const me = useMeStore((state) => state.me);
  const username = displayName(me) || tokenUsername;
  const showAdmin = useCanEnterAdmin();

  return (
    <>
      <ChromePill>
        <DropdownMenu modal={false}>
          <ChromeTooltip label="更多" side="bottom">
            <DropdownMenuTrigger render={<ChromeButton aria-label="更多" />}>
              <MoreHorizontal />
            </DropdownMenuTrigger>
          </ChromeTooltip>
          <DropdownMenuContent align="end" sideOffset={10} className="w-56">
            <DropdownMenuItem onClick={onOpenSettings}>
              <Settings />
              画布设置
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onOpenShortcuts}>
              <Keyboard />
              快捷键
              <DropdownMenuShortcut>?</DropdownMenuShortcut>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
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
          </DropdownMenuContent>
        </DropdownMenu>
      </ChromePill>

      <CreditsPill />

      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          aria-label="账户"
          className={cn(
            "bg-chrome ring-chrome-border grid size-10 place-items-center rounded-full text-sm font-semibold uppercase shadow-lg ring-1 backdrop-blur-xl",
            "hover:ring-node-ring/40 focus-visible:ring-node-ring/60 transition-shadow outline-none focus-visible:ring-2",
          )}
        >
          {me ? (
            <UserAvatar userId={me.id} name={username ?? ""} src={me.avatarUrl} size={40} />
          ) : (
            (username?.slice(0, 1) ?? "我")
          )}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" sideOffset={10} className="w-52">
          {username && (
            <DropdownMenuGroup>
              <DropdownMenuLabel className="truncate">{username}</DropdownMenuLabel>
            </DropdownMenuGroup>
          )}
          <DropdownMenuItem onClick={() => navigate("/profile")}>
            <User />
            个人中心
          </DropdownMenuItem>
          {showAdmin && (
            <DropdownMenuItem onClick={() => navigate("/admin/ai")}>
              <ShieldCheck />
              管理后台
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={logout}>
            <LogOut />
            退出登录
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );
}
