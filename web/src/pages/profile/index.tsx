import { useEffect } from "react";
import { useSearchParams } from "react-router";
import { CalendarDays, PenLine, RotateCcw } from "lucide-react";

import { UserAvatar } from "@/components/user-avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useMeStore } from "@/store/me";
import {
  displayName,
  joinedText,
  parseProfileTab,
  type ProfileTab,
} from "@/utils/profile/profile-rules";

import { CreditsTab } from "./credits-tab";
import { OverviewTab } from "./overview-tab";
import { ProfileTab as ProfileFormTab } from "./profile-tab";
import { SecurityTab } from "./security-tab";

/** tab 的显示名，顺序即界面顺序 */
const TABS: { value: ProfileTab; label: string }[] = [
  { value: "overview", label: "概览" },
  { value: "credits", label: "积分" },
  { value: "profile", label: "资料" },
  { value: "security", label: "安全" },
];

/** 角色徽标：普通用户不显示 */
const ROLE_LABEL: Record<string, string> = {
  admin: "管理员",
  super_admin: "超级管理员",
};

/**
 * 身份卡：头像、昵称（空时回落到用户名）、@用户名 · 邮箱、角色徽标、加入时间，右侧「编辑资料」。
 * 加载中是骨架；失败显示「加载失败，重试」，不影响其他 tab。
 * @param onEdit 切到「资料」tab
 */
function IdentityCard({ onEdit }: { onEdit: () => void }) {
  const me = useMeStore((state) => state.me);
  const status = useMeStore((state) => state.status);
  const fetchMe = useMeStore((state) => state.fetchMe);

  if (!me) {
    if (status === "error") {
      return (
        <div className="bg-card ring-foreground/10 flex items-center gap-3 rounded-xl p-5 ring-1">
          <span className="text-muted-foreground text-sm">个人资料加载失败</span>
          <Button variant="outline" size="sm" onClick={() => void fetchMe()}>
            <RotateCcw />
            重试
          </Button>
        </div>
      );
    }
    return (
      <div
        className="bg-card ring-foreground/10 flex items-center gap-4 rounded-xl p-5 ring-1"
        aria-busy
      >
        <Skeleton className="size-16 rounded-full" />
        <div className="grid flex-1 gap-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-56" />
        </div>
      </div>
    );
  }

  const name = displayName(me);
  const role = ROLE_LABEL[me.role];
  return (
    <div className="bg-card ring-foreground/10 flex flex-wrap items-center gap-4 rounded-xl p-5 ring-1">
      <button
        type="button"
        onClick={onEdit}
        aria-label="编辑头像"
        title="编辑头像"
        className="focus-visible:ring-ring/50 rounded-full transition-opacity outline-none hover:opacity-85 focus-visible:ring-3"
      >
        <UserAvatar userId={me.id} name={name} src={me.avatarUrl} size={64} />
      </button>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <h1 className="truncate text-xl font-semibold tracking-tight">{name}</h1>
          {role && <Badge variant="secondary">{role}</Badge>}
        </div>
        <p className="text-muted-foreground truncate text-sm">
          @{me.username}
          {me.email && ` · ${me.email}`}
        </p>
        <p className="text-muted-foreground mt-0.5 flex items-center gap-1 text-xs tabular-nums">
          <CalendarDays className="size-3.5" />
          {joinedText(me.createdAt)}
        </p>
      </div>
      <Button variant="outline" size="sm" onClick={onEdit}>
        <PenLine />
        编辑资料
      </Button>
    </div>
  );
}

/**
 * 个人中心（设计 docs/design/个人中心）：顶部身份卡 + 概览 / 积分 / 资料 / 安全四个 tab。
 * 当前 tab 同步到 ?tab=，积分页的页码同步到 ?page=，刷新后停在原处，也能直达 /profile?tab=security。
 */
export default function ProfilePage() {
  const [params, setParams] = useSearchParams();
  const tab = parseProfileTab(params.get("tab"));
  const fetchMe = useMeStore((state) => state.fetchMe);
  const hasMe = useMeStore((state) => !!state.me);

  /** 启动时已经拉过；直接打开这一页又恰好还没拉到时补一次 */
  useEffect(() => {
    if (!hasMe) void fetchMe();
  }, [hasMe, fetchMe]);

  const goTab = (next: ProfileTab) => {
    if (next === tab) return;
    setParams(next === "overview" ? {} : { tab: next });
  };

  return (
    <div className="mx-auto grid w-full max-w-260 gap-5 px-4 pt-1 pb-24 md:px-6">
      <IdentityCard onEdit={() => goTab("profile")} />
      <Tabs value={tab} onValueChange={(value) => goTab(parseProfileTab(String(value)))}>
        <TabsList variant="line" aria-label="个人中心">
          {TABS.map((item) => (
            <TabsTrigger key={item.value} value={item.value} className="px-3">
              {item.label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="overview" className="pt-3">
          <OverviewTab />
        </TabsContent>
        <TabsContent value="credits" className="pt-3">
          <CreditsTab />
        </TabsContent>
        <TabsContent value="profile" className="pt-3">
          <ProfileFormTab />
        </TabsContent>
        <TabsContent value="security" className="pt-3">
          <SecurityTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}
