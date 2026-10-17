import { useState } from "react";
import { toast } from "sonner";

import { disablePaymentChannel } from "@/api/admin/payment";
import type { PaymentChannel } from "@/api/admin/payment/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { disableConsequence } from "@/utils/admin/payment/channel";

import { ChannelSheet } from "./channel-sheet";
import { ChannelTable } from "./channel-table";
import { SettingsCard } from "./settings-card";
import type { PaymentChannels } from "./use-payment-channels";

/**
 * 「支付渠道」页签：顶部全局设置卡 + 四个内置渠道的列表 + 配置抽屉。
 * 全局设置和渠道的写操作只对 super_admin 开放，admin 看得到但按钮禁用并写明原因。
 * 请求错误的全局提示由拦截器弹，这里不重复。
 * @param channels 页面入口持有的渠道列表状态
 * @param canWrite 是否有写权限（super_admin）
 */
export function ChannelsTab({
  channels,
  canWrite,
}: {
  channels: PaymentChannels;
  canWrite: boolean;
}) {
  const { channels: list, status, reload, retry, replace } = channels;
  /** 抽屉当前对应的渠道标识；关闭时只收起抽屉、保留标识，退出动画里内容不会先消失 */
  const [provider, setProvider] = useState<string | null>(null);
  const [open, setOpen] = useState(false);

  const disable = (channel: PaymentChannel) =>
    confirm({
      title: `停用「${channel.name}」？`,
      destructive: true,
      confirmLabel: "停用",
      description: disableConsequence(channel),
      onConfirm: async () => {
        const view = await disablePaymentChannel(channel.provider);
        toast.success(`「${channel.name}」已停用`);
        replace(view);
        void reload();
      },
    });

  return (
    <div className="flex flex-col gap-4">
      <SettingsCard channels={list} canWrite={canWrite} onSaved={() => void reload()} />
      <section className="bg-card overflow-hidden rounded-xl border">
        <ChannelTable
          channels={list}
          status={status}
          canWrite={canWrite}
          onRetry={retry}
          actions={{
            onOpen: (channel) => {
              setProvider(channel.provider);
              setOpen(true);
            },
            onDisable: (channel) => void disable(channel),
          }}
        />
      </section>
      <ChannelSheet
        channel={list.find((item) => item.provider === provider) ?? null}
        open={open}
        canWrite={canWrite}
        onClose={() => setOpen(false)}
        reload={reload}
        replace={replace}
      />
    </div>
  );
}
