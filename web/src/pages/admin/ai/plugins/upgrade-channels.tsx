import { toast } from "sonner";

import { updateChannel } from "@/api/admin/ai";
import type { ChannelView, PluginView } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { upgradeChannelRequest } from "@/utils/admin/channel-form";
import { errorMessage } from "@/utils/admin/errors";
import { compareSemver, latestVersion } from "@/utils/admin/plugin";

/** 固定在插件旧版本上的渠道（可以升级到最新版本的） */
export const outdatedChannels = (plugin: PluginView, channels: readonly ChannelView[]) => {
  const latest = latestVersion(plugin);
  if (!latest) return [];
  return channels.filter(
    (channel) =>
      channel.plugin_key === plugin.key &&
      compareSemver(channel.plugin_version, latest.version) < 0,
  );
};

/**
 * 一键把插件的所有旧版本渠道升级到最新版本（插件页、总览页共用，走全局确认框）：
 * 确认框里列出每个渠道从哪个版本升到哪个、会丢弃哪些设置项；确认后逐个更新，
 * 有失败的不中断，最后把失败原因留在框里（成功的已经生效）。
 * @param onDone 有渠道升级成功后（刷新渠道清单）
 * @returns 是否全部成功
 */
export function confirmUpgradeChannels({
  plugin,
  channels,
  plugins,
  onDone,
}: {
  plugin: PluginView;
  channels: readonly ChannelView[];
  plugins: readonly PluginView[];
  onDone: () => Promise<unknown> | void;
}) {
  const latest = latestVersion(plugin);
  const targets = outdatedChannels(plugin, channels);
  if (!latest || targets.length === 0) return Promise.resolve(false);
  const plans = targets.map((channel) => ({
    channel,
    plan: upgradeChannelRequest(channel, plugins, latest.version),
  }));

  return confirm({
    title: `把 ${targets.length} 个渠道升级到 v${latest.version}？`,
    confirmLabel: "全部升级",
    description: "升级后新任务用新版本，进行中的任务按旧版本跑完。建议升级后对相关模型试跑一次。",
    children: (
      <ul className="bg-muted/40 flex max-h-56 flex-col gap-1.5 overflow-y-auto rounded-lg border p-3 text-xs">
        {plans.map(({ channel, plan }) => (
          <li key={channel.key}>
            <span className="font-medium">{channel.name}</span>
            <span className="text-muted-foreground font-mono">
              {" "}
              v{channel.plugin_version} → v{latest.version}
            </span>
            {plan && plan.dropped.length > 0 && (
              <span className="block text-amber-600 dark:text-amber-400">
                新版本不再有这些设置，会丢弃：{plan.dropped.join("、")}
              </span>
            )}
            {plan?.credsReset && (
              <span className="block text-amber-600 dark:text-amber-400">
                新版本不需要读 Key，会关闭“允许插件读取 Key”
              </span>
            )}
          </li>
        ))}
      </ul>
    ),
    onConfirm: async () => {
      const failed: string[] = [];
      let done = 0;
      for (const { channel, plan } of plans) {
        if (!plan) {
          failed.push(`${channel.name}：找不到目标版本`);
          continue;
        }
        try {
          await updateChannel(channel.key, plan.update);
          done += 1;
        } catch (error) {
          failed.push(`${channel.name}：${errorMessage(error, "升级失败")}`);
        }
      }
      if (done > 0) {
        toast.success(`${done} 个渠道已升级到 v${latest.version}`);
        await onDone();
      }
      if (failed.length > 0) throw new Error(failed.join("；"));
    },
  });
}
