import { AnimatePresence, motion } from "motion/react";
import {
  CircleArrowUp,
  KeyRound,
  PartyPopper,
  PlugZap,
  RadioTower,
  type LucideIcon,
} from "lucide-react";
import { useNavigate } from "react-router";

import { listChannelOptions, listPluginOptions } from "@/api/admin/ai";
import type { OverviewTodo } from "@/api/admin/ai/type";
import { Tag, toneClasses } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT, SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

import { openSecretDialog } from "../channels/secret-dialog";
import { confirmUpgradeChannels } from "../plugins/upgrade-channels";

const TODO_TONE = { bad: "danger", warn: "warning", info: "info" } as const;
const TODO_ICON: Record<OverviewTodo["action"]["kind"], LucideIcon> = {
  "set-key": KeyRound,
  "open-plugin": PlugZap,
  "open-channel": RadioTower,
  "upgrade-plugin": CircleArrowUp,
};

/**
 * 待处理清单：数据来自后端汇总（GET /admin/ai/overview 的 todos），每条一个修复按钮，能就地修的就地修（设置 Key、一键升级），其余深链到对应位置。
 * - 修好之后清单重新计算，这一条向右淡出（DURATION.exit），其余行用 layout 补位；
 * - flash 变化时描边闪一下（总览顶部的“N 项待处理”点击后触发）；
 * - 没有写权限（非 super_admin）时按钮降级为“查看 / 去插件页”。
 * @param todos 待处理清单，没就绪时传空数组
 * @param ready 汇总已加载
 * @param canWrite 是否能管理渠道与插件
 * @param onChanged 就地修复（设置 Key、一键升级）成功后，刷新汇总
 * @param flash 每变一次闪一次描边，0 表示不闪
 */
export function TodoList({
  todos,
  ready,
  canWrite,
  onChanged,
  flash,
}: {
  todos: OverviewTodo[];
  ready: boolean;
  canWrite: boolean;
  onChanged: () => void;
  flash: number;
}) {
  const navigate = useNavigate();

  const run = async ({ action }: OverviewTodo) => {
    const channelKey = encodeURIComponent(action.channel_key ?? "");
    const pluginKey = encodeURIComponent(action.plugin_key ?? "");
    if (action.kind === "set-key") {
      // 待处理里的“设置 Key”一定是还没设置过 Key 的渠道，名字由后端一并带来
      if (canWrite && action.channel_key) {
        openSecretDialog({
          channelKey: action.channel_key,
          channelName: action.channel_name ?? action.channel_key,
          secretSet: false,
          onSaved: onChanged,
        });
        return;
      }
      navigate(`/admin/ai/channels?key=${channelKey}`);
    } else if (action.kind === "open-plugin") {
      navigate(`/admin/ai/plugins?key=${pluginKey}`);
    } else if (action.kind === "upgrade-plugin") {
      if (canWrite) {
        // 升级要对照全量插件版本和渠道，点了才取（按需，不预加载）
        try {
          const [plugins, channels] = await Promise.all([listPluginOptions(), listChannelOptions()]);
          const plugin = plugins.find((item) => item.key === action.plugin_key);
          if (plugin) {
            void confirmUpgradeChannels({ plugin, channels, plugins, onDone: onChanged });
            return;
          }
        } catch {
          // 请求失败的全局 toast 已弹，退回到去插件页
        }
      }
      navigate(`/admin/ai/plugins?key=${pluginKey}`);
    } else {
      navigate(`/admin/ai/channels?key=${channelKey}`);
    }
  };

  const label = ({ action }: OverviewTodo) =>
    action.kind === "set-key"
      ? canWrite
        ? "设置 Key"
        : "查看渠道"
      : action.kind === "open-plugin"
        ? "去插件页"
        : action.kind === "open-channel"
          ? "查看渠道"
          : canWrite
            ? "一键升级"
            : "去插件页";

  return (
    <Card data-slot="todo-list" className="relative h-full gap-0 py-0">
      <CardHeader className="flex-row items-center gap-2 border-b py-3">
        <CardTitle>待处理</CardTitle>
        {ready && <Tag className="tabular-nums">{todos.length}</Tag>}
      </CardHeader>

      {!ready && (
        <div className="space-y-2 p-4">
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
        </div>
      )}
      {ready && todos.length === 0 && (
        <div className="text-muted-foreground flex flex-1 flex-col items-center justify-center gap-2 p-10 text-sm">
          <PartyPopper className="size-5" />
          一切正常，没有要处理的事
        </div>
      )}
      {ready && todos.length > 0 && (
        <ul>
          <AnimatePresence initial={false}>
            {todos.map((todo) => {
              const Icon = TODO_ICON[todo.action.kind];
              return (
                <motion.li
                  key={todo.id}
                  layout="position"
                  transition={SPRING}
                  exit={{
                    opacity: 0,
                    x: 8,
                    transition: { duration: DURATION.exit, ease: EASE_OUT },
                  }}
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
                  <span className="min-w-0 flex-1 text-sm leading-snug">{todo.text}</span>
                  <Button
                    size="sm"
                    variant="outline"
                    className="shrink-0"
                    onClick={() => void run(todo)}
                  >
                    {label(todo)}
                  </Button>
                </motion.li>
              );
            })}
          </AnimatePresence>
        </ul>
      )}

      {flash > 0 && (
        <motion.span
          key={flash}
          aria-hidden
          initial={{ opacity: 0 }}
          animate={{ opacity: [0, 1, 1, 0] }}
          transition={{ duration: 1.2, times: [0, 0.1, 0.6, 1], ease: EASE_OUT }}
          className="pointer-events-none absolute inset-0 rounded-xl ring-2 ring-amber-500"
        />
      )}
    </Card>
  );
}
