import { useState } from "react";
import { toast } from "sonner";

import { setPluginEnabled } from "@/api/admin/ai";
import type { ChannelView, ConfigListItem, PluginView } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";

import { useAliveRef } from "../../use-admin";

/**
 * 插件的启停：表格行里的开关和弹窗头部的开关共用同一份逻辑。
 * 启用直接生效；停用影响面大（所有用它的渠道、渠道下的在线模型），先算出影响再确认。
 * @param channels 全部渠道，算停用会波及几个
 * @param models 全部模型，算波及几个在线模型；没加载到时为空数组
 * @param onChanged 启停成功后刷新插件清单
 * @returns toggling 正在切换的插件 key（切换中开关禁用）；toggle 开关的 onCheckedChange
 */
export function usePluginActions({
  channels,
  models,
  onChanged,
}: {
  channels: ChannelView[];
  models: ConfigListItem[];
  onChanged: () => Promise<void>;
}) {
  const aliveRef = useAliveRef();
  const [toggling, setToggling] = useState<string | null>(null);

  const setEnabled = async (plugin: PluginView, enabled: boolean) => {
    setToggling(plugin.key);
    try {
      await setPluginEnabled(plugin.key, enabled);
      toast.success(`插件「${plugin.name}」已${enabled ? "启用" : "停用"}`);
      await onChanged();
    } finally {
      if (aliveRef.current) setToggling(null);
    }
  };

  const toggle = (plugin: PluginView, enabled: boolean) => {
    if (enabled) return void setEnabled(plugin, true);
    const pinned = channels.filter((channel) => channel.plugin_key === plugin.key);
    const online = models.filter(
      (item) => item.enabled && pinned.some((channel) => channel.key === item.channel),
    );
    void confirm({
      title: `停用插件「${plugin.name}」？`,
      destructive: true,
      confirmLabel: "停用",
      description: (
        <>
          停用后{" "}
          <b>
            {pinned.length} 个渠道{online.length > 0 && `、${online.length} 个在线模型`}
          </b>{" "}
          会不可用，进行中的任务不受影响。
        </>
      ),
      onConfirm: () => setEnabled(plugin, false),
    });
  };

  return { toggling, toggle };
}
