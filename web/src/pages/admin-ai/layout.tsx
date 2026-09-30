import { useEffect } from "react";
import { Link, NavLink, Outlet } from "react-router";
import { ArrowLeft } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { ROLE_LABEL } from "@/utils/admin/role";

import { ForbiddenView, Tag } from "./shared";
import { useAdminCatalog, type AdminOutletContext } from "./use-admin";

/** 三个标签：与运营的使用频率一致（模型最常用） */
const TABS = [
  { to: "models", label: "模型" },
  { to: "channels", label: "渠道" },
  { to: "plugins", label: "插件" },
] as const;

/**
 * 管理端 AI 配置的共用布局：进入时取一次角色放进 useAdminStore，
 * 未确认前整页骨架（不闪 403），403 显示兜底页，确认后渲染标签导航与子路由。
 * 插件 / 渠道清单在这里加载一次，通过 Outlet 上下文给三个子页共用。
 */
export default function AdminAiLayout() {
  const role = useAdminStore((state) => state.role);
  const status = useAdminStore((state) => state.status);
  const load = useAdminStore((state) => state.load);

  useEffect(() => {
    void load();
  }, [load]);

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
      <div className="flex h-svh flex-col" aria-busy="true" aria-label="正在确认管理权限">
        <div className="flex h-12 shrink-0 items-center gap-3 border-b px-3">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-6 w-48" />
        </div>
        <div className="grid flex-1 gap-4 p-6 md:grid-cols-[16rem_1fr]">
          <Skeleton className="h-64" />
          <Skeleton className="h-96" />
        </div>
      </div>
    );
  }

  const context: AdminOutletContext = { catalog };
  const counts: Record<string, number | null> = {
    models: null,
    channels: catalog.channelsStatus === "ready" ? catalog.channels.length : null,
    plugins: catalog.pluginsStatus === "ready" ? catalog.plugins.length : null,
  };

  return (
    <div className="flex h-svh flex-col">
      <header className="flex h-12 shrink-0 items-center gap-3 border-b px-3">
        <Link
          to="/"
          className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm"
        >
          <ArrowLeft className="size-4" />
          返回画布
        </Link>
        <h1 className="text-sm font-medium">AI 配置管理</h1>
        <nav aria-label="AI 配置" className="bg-muted flex gap-0.5 rounded-lg p-0.5 text-xs">
          {TABS.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              className={({ isActive }) =>
                cn(
                  "rounded-md px-3 py-1",
                  isActive
                    ? "bg-background shadow-sm"
                    : "text-muted-foreground hover:text-foreground",
                )
              }
            >
              {tab.label}
              {counts[tab.to] != null && (
                <span className="text-muted-foreground ml-1">{counts[tab.to]}</span>
              )}
            </NavLink>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <Tag tone={role === "super_admin" ? "info" : "neutral"} title="当前管理端角色">
            {ROLE_LABEL[role]}
          </Tag>
        </div>
      </header>
      <div className="min-h-0 flex-1">
        <Outlet context={context} />
      </div>
    </div>
  );
}
