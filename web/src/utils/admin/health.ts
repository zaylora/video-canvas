import type { ChannelView, ConfigListItem, PluginView } from "@/api/admin/ai/type";

import { availableUpgrade, channelMeta } from "./plugin";

/** 健康状态的色调：ok 绿、bad 红、warn 黄、off 灰 */
export type HealthTone = "ok" | "bad" | "warn" | "off";

/** 健康色调对应到 Tag / StatusLabel 的 tone */
export const HEALTH_UI_TONE = {
  ok: "success",
  bad: "danger",
  warn: "warning",
  off: "neutral",
} as const satisfies Record<HealthTone, string>;

export type ChannelHealth = {
  tone: HealthTone;
  /** 列表里的短标签 */
  label: string;
  /** 不可用的原因（整句）；可用时为 null */
  reason: string | null;
};

/**
 * 渠道实际能不能用：插件停用 / 插件不存在 / 缺 Key 都会让渠道不可用，即使渠道本身是“启用”。
 * 插件信息读不到（清单没加载、版本已删）时不断言缺 Key。
 */
export function channelHealth(channel: ChannelView, plugins: readonly PluginView[]): ChannelHealth {
  if (!channel.enabled) return { tone: "off", label: "已停用", reason: "渠道已停用" };
  const plugin = plugins.find((item) => item.key === channel.plugin_key);
  if (!plugin) {
    return { tone: "bad", label: "不可用", reason: `插件 ${channel.plugin_key} 不存在` };
  }
  if (!plugin.enabled) {
    return { tone: "bad", label: "不可用", reason: `插件「${plugin.name}」已停用` };
  }
  const meta = channelMeta(plugins, channel);
  if (meta && (meta.auth?.type ?? "none") !== "none" && !channel.secret_set) {
    return { tone: "warn", label: "未设 Key", reason: "还没有设置 Key" };
  }
  return { tone: "ok", label: "可用", reason: null };
}

/** 模型对运营只露一个状态 */
export type ModelStatus = "online" | "broken" | "offline";

export const MODEL_STATUS_LABEL: Record<ModelStatus, string> = {
  online: "在线",
  broken: "不可用",
  offline: "未上线",
};

export const MODEL_STATUS_TONE: Record<ModelStatus, HealthTone> = {
  online: "ok",
  broken: "bad",
  offline: "off",
};

export type ModelHealth = {
  status: ModelStatus;
  /**
   * 渠道层面的问题（整句）：在线以外的状态也可能有，比如“未上线”的模型渠道缺 Key，
   * 界面据此提示“上线前要先处理”；没有问题为 null
   */
  reason: string | null;
};

/**
 * 模型对用户的真实状态：
 * - 未上线：没启用（保存后默认就是这个状态），用户看不到；
 * - 不可用：已启用，但渠道不存在 / 停用 / 插件停用 / 缺 Key，用户实际用不了；
 * - 在线：其余。
 * @param channelsReady 渠道与插件清单都已加载；没加载完不下“不可用”的结论
 */
export function modelHealth(
  item: ConfigListItem,
  channels: readonly ChannelView[],
  plugins: readonly PluginView[],
  channelsReady = true,
): ModelHealth {
  let reason: string | null = null;
  if (channelsReady) {
    const channel = item.channel ? channels.find((c) => c.key === item.channel) : undefined;
    if (!item.channel) reason = "还没有选择渠道";
    else if (!channel) reason = `渠道 ${item.channel} 不存在`;
    else {
      const health = channelHealth(channel, plugins);
      if (health.reason) reason = `渠道「${channel.name}」${health.reason}`;
    }
  }
  if (!item.enabled) return { status: "offline", reason };
  return { status: reason ? "broken" : "online", reason };
}

/** 总览页“待处理”的一条 */
export type AdminTodo = {
  /** 稳定 id（React key） */
  id: string;
  tone: Exclude<HealthTone, "ok" | "off"> | "info";
  text: string;
  /** 修复动作 */
  action:
    | { kind: "set-key"; channelKey: string }
    | { kind: "open-plugin"; pluginKey: string }
    | { kind: "open-channel"; channelKey: string }
    | { kind: "upgrade-plugin"; pluginKey: string };
};

/**
 * 总览页的待处理清单，按严重度排：插件停用波及在线模型 → 渠道问题导致模型不可用 → 缺 Key →
 * 插件可升级。
 */
export function adminTodos(
  models: readonly ConfigListItem[],
  channels: readonly ChannelView[],
  plugins: readonly PluginView[],
): AdminTodo[] {
  const todos: AdminTodo[] = [];
  const usedBy = (channelKey: string) => models.filter((m) => m.channel === channelKey);

  for (const plugin of plugins) {
    const pinned = channels.filter((c) => c.plugin_key === plugin.key);
    if (plugin.enabled || pinned.length === 0) continue;
    const hit = models.filter((m) => m.enabled && pinned.some((c) => c.key === m.channel));
    todos.push({
      id: `plugin-off:${plugin.key}`,
      tone: "bad",
      text: `插件「${plugin.name}」已停用，${pinned.length} 个渠道${hit.length ? `、${hit.length} 个上架模型` : ""}实际不可用`,
      action: { kind: "open-plugin", pluginKey: plugin.key },
    });
  }

  for (const channel of channels) {
    const health = channelHealth(channel, plugins);
    const used = usedBy(channel.key);
    if (health.label === "未设 Key") {
      todos.push({
        id: `key:${channel.key}`,
        tone: "warn",
        text: `渠道「${channel.name}」还没设置 Key${used.length ? `，${used.length} 个模型因此无法上线` : ""}`,
        action: { kind: "set-key", channelKey: channel.key },
      });
    } else if (!channel.enabled) {
      const online = used.filter((m) => m.enabled);
      if (online.length > 0) {
        todos.push({
          id: `channel-off:${channel.key}`,
          tone: "bad",
          text: `渠道「${channel.name}」已停用，${online.length} 个上架模型实际不可用`,
          action: { kind: "open-channel", channelKey: channel.key },
        });
      }
    }
  }

  // 可升级按插件合并成一条：总览页上一键把这个插件的旧版本渠道全部升级
  for (const plugin of plugins) {
    const outdated = channels.filter(
      (channel) => channel.plugin_key === plugin.key && availableUpgrade(plugins, channel),
    );
    const latest = outdated.length ? availableUpgrade(plugins, outdated[0]) : null;
    if (!latest) continue;
    todos.push({
      id: `upgrade:${plugin.key}`,
      tone: "info",
      text: `${plugin.name} v${latest.version} 可用，${outdated.length} 个渠道还在旧版本`,
      action: { kind: "upgrade-plugin", pluginKey: plugin.key },
    });
  }

  const order = { bad: 0, warn: 1, info: 2 } as const;
  return todos.sort((a, b) => order[a.tone] - order[b.tone]);
}
