import {
  Check,
  ChevronRight,
  CircleAlert,
  CircleArrowUp,
  KeyRound,
  PartyPopper,
  Plus,
  PlugZap,
  RadioTower,
  type LucideIcon,
} from "lucide-react";
import { useNavigate } from "react-router";

import {
  PageHeader,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Tag, toneClasses } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { useAdminStore } from "@/store/admin";
import { adminTodos, channelHealth, modelHealth, type AdminTodo } from "@/utils/admin/health";
import { canManageInfra } from "@/utils/admin/role";

import { openSecretDialog } from "../channels/secret-dialog";
import { confirmUpgradeChannels } from "../plugins/upgrade-channels";
import { useAdminOutlet, useModelList } from "../../use-admin";

const TODO_TONE = { bad: "danger", warn: "warning", info: "info" } as const;
const TODO_ICON: Record<AdminTodo["action"]["kind"], LucideIcon> = {
  "set-key": KeyRound,
  "open-plugin": PlugZap,
  "open-channel": RadioTower,
  "open-models": CircleAlert,
  "upgrade-plugin": CircleArrowUp,
};

/**
 * 总览页（后台首页）：一眼看清“系统能不能用、哪里要处理”。
 * - 上线进度：接入平台（插件）→ 配置渠道 → 上线模型，卡住的那一步高亮并给主按钮；
 * - 待处理：插件停用波及模型、渠道缺 Key、渠道停用、插件可升级、有未上线的修改，
 *   每条一个修复按钮，能就地修的就地修（设置 Key），其余深链到对应位置。
 * 数据全部来自现有清单接口，在前端计算（见 utils/admin/health.ts）。
 */
export default function OverviewPage() {
  const { catalog } = useAdminOutlet();
  const { models, status: modelsStatus } = useModelList();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();

  const ready =
    catalog.pluginsStatus === "ready" &&
    catalog.channelsStatus === "ready" &&
    modelsStatus === "ready";
  const usablePlugins = catalog.plugins.filter((item) => item.enabled).length;
  const usableChannels = catalog.channels.filter(
    (item) => channelHealth(item, catalog.plugins).tone === "ok",
  ).length;
  const online = models.filter(
    (item) => modelHealth(item, catalog.channels, catalog.plugins).status === "online",
  ).length;
  const todos = ready ? adminTodos(models, catalog.channels, catalog.plugins) : [];

  const steps = [
    {
      title: "接入平台",
      hint: "插件",
      ok: usablePlugins > 0,
      stat: `${usablePlugins} 个可用`,
      action: canWrite ? { label: "上传插件", to: "/admin/ai/plugins" } : null,
    },
    {
      title: "配置渠道",
      hint: "地址 + Key",
      ok: usableChannels > 0,
      stat: `${usableChannels} / ${catalog.channels.length} 个可用`,
      action: canWrite ? { label: "新建渠道", to: "/admin/ai/channels?edit=new" } : null,
    },
    {
      title: "上线模型",
      hint: "用户能在画布里选到",
      ok: online > 0,
      stat: `${online} 个在线`,
      action: { label: "新建模型", to: "/admin/ai/models/new" },
    },
  ];
  const current = steps.find((step) => !step.ok);

  const runTodo = ({ action }: AdminTodo) => {
    if (action.kind === "set-key") {
      const channel = catalog.channels.find((item) => item.key === action.channelKey);
      if (canWrite && channel) {
        openSecretDialog({
          channelKey: channel.key,
          channelName: channel.name,
          secretSet: channel.secret_set,
          onSaved: () => void catalog.reloadChannels(),
        });
        return;
      }
      navigate(`/admin/ai/channels?key=${encodeURIComponent(action.channelKey)}`);
    } else if (action.kind === "open-plugin") {
      navigate(`/admin/ai/plugins?key=${encodeURIComponent(action.pluginKey)}`);
    } else if (action.kind === "upgrade-plugin") {
      const plugin = catalog.plugins.find((item) => item.key === action.pluginKey);
      if (canWrite && plugin) {
        void confirmUpgradeChannels({
          plugin,
          channels: catalog.channels,
          plugins: catalog.plugins,
          onDone: catalog.reloadChannels,
        });
        return;
      }
      navigate(`/admin/ai/plugins?key=${encodeURIComponent(action.pluginKey)}`);
    } else if (action.kind === "open-channel") {
      navigate(`/admin/ai/channels?key=${encodeURIComponent(action.channelKey)}`);
    } else {
      navigate(`/admin/ai/models?status=${encodeURIComponent(action.filter)}`);
    }
  };
  const todoLabel = ({ action }: AdminTodo) =>
    action.kind === "set-key"
      ? canWrite
        ? "设置 Key"
        : "查看渠道"
      : action.kind === "open-plugin"
        ? "去插件页"
        : action.kind === "open-channel"
          ? "查看渠道"
          : action.kind === "upgrade-plugin"
            ? canWrite
              ? "一键升级"
              : "去插件页"
            : "去查看";

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>总览</PageHeaderTitle>
            <PageHeaderDescription>
              现在系统能不能用、哪里需要处理，一眼看完。上线一个模型要走三步：接入平台 → 配置渠道 →
              上线模型。
            </PageHeaderDescription>
          </PageHeaderHeading>
        </PageHeader>

        <section aria-label="上线进度" className="grid gap-3 md:grid-cols-3">
          {steps.map(({ action, ...step }, index) => (
            <Card
              key={step.title}
              className={cn(
                "relative gap-3",
                current?.title === step.title && "ring-primary ring-2",
              )}
            >
              <CardHeader className="flex-row items-center gap-2">
                <span
                  className={cn(
                    "grid size-6 shrink-0 place-items-center rounded-full text-xs font-semibold",
                    step.ok ? "bg-emerald-500 text-white" : "bg-muted",
                  )}
                >
                  {step.ok ? <Check className="size-3.5" /> : index + 1}
                </span>
                <CardTitle className="text-sm">{step.title}</CardTitle>
                <span className="text-muted-foreground text-xs">{step.hint}</span>
              </CardHeader>
              <CardContent className="flex flex-col items-start gap-3">
                {ready ? (
                  <span className="text-2xl font-semibold tabular-nums">{step.stat}</span>
                ) : (
                  <Skeleton className="h-8 w-28" />
                )}
                {action && (
                  <Button
                    size="sm"
                    variant={current?.title === step.title ? "default" : "outline"}
                    onClick={() => navigate(action.to)}
                  >
                    <Plus />
                    {action.label}
                  </Button>
                )}
              </CardContent>
              {index < steps.length - 1 && (
                <ChevronRight className="text-muted-foreground absolute top-1/2 -right-3 hidden size-4 -translate-y-1/2 md:block" />
              )}
            </Card>
          ))}
        </section>

        <Card className="mt-6 gap-0 py-0">
          <CardHeader className="flex-row items-center gap-2 border-b py-3">
            <CardTitle className="text-base">待处理</CardTitle>
            {ready && <Tag>{todos.length}</Tag>}
          </CardHeader>
          {!ready && (
            <div className="space-y-2 p-4">
              <Skeleton className="h-10" />
              <Skeleton className="h-10" />
            </div>
          )}
          {ready && todos.length === 0 && (
            <div className="text-muted-foreground flex flex-col items-center gap-2 p-10 text-sm">
              <PartyPopper className="size-5" />
              一切正常，没有要处理的事
            </div>
          )}
          {ready && todos.length > 0 && (
            <ul>
              {todos.map((todo) => {
                const Icon = TODO_ICON[todo.action.kind];
                return (
                  <li
                    key={todo.id}
                    className="flex items-center gap-3 border-b px-4 py-3 last:border-b-0"
                  >
                    <span
                      className={cn(
                        "grid size-8 shrink-0 place-items-center rounded-md border",
                        toneClasses[TODO_TONE[todo.tone]],
                      )}
                    >
                      <Icon className="size-4" />
                    </span>
                    <span className="flex-1 text-sm">{todo.text}</span>
                    <Button size="sm" variant="outline" onClick={() => runTodo(todo)}>
                      {todoLabel(todo)}
                    </Button>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>
      </main>
    </div>
  );
}
