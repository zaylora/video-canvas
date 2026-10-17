import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { enablePaymentChannel } from "@/api/admin/payment";
import type { PaymentChannel } from "@/api/admin/payment/type.d";
import {
  CopyBlock,
  CopyBlockCode,
  CopyBlockHeader,
  CopyBlockTitle,
} from "@/components/admin-ui/copy-block";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { Notice } from "@/components/admin-ui/notice";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Button } from "@/components/ui/button";
import { SheetFooter } from "@/components/ui/sheet";
import {
  OPS_ONLY_REASON,
  enableBlockReason,
  notifyUrlProblem,
} from "@/utils/admin/payment/channel";

import { useAliveRef } from "../use-admin";

/**
 * 配置抽屉第 3 步「启用」：展示需要在渠道商户后台登记的回调地址，再启用渠道。
 * 没有公网地址或（微信）不是 https 时红色提示并禁用启用按钮，后端同样会拒绝（65013 / 65009）。
 * 启用只是渠道可用，前台要用到还得在全局设置里把微信 / 支付宝指给它。
 * @param channel 渠道视图（列表里的最新一份）
 * @param canWrite 是否有写权限（super_admin）
 * @param onBack 回到第 2 步
 * @param onClose 关闭抽屉
 * @param reload 重新拉取渠道列表
 * @param replace 用返回的渠道视图就地替换列表项
 */
export function ChannelEnableStep({
  channel,
  canWrite,
  onBack,
  onClose,
  reload,
  replace,
}: {
  channel: PaymentChannel;
  canWrite: boolean;
  onBack: () => void;
  onClose: () => void;
  reload: () => Promise<PaymentChannel[] | null>;
  replace: (view: PaymentChannel) => void;
}) {
  const aliveRef = useAliveRef();
  const [enabling, setEnabling] = useState(false);
  const problem = notifyUrlProblem(channel);
  const blockReason = canWrite ? enableBlockReason(channel) : OPS_ONLY_REASON;

  const enable = async () => {
    if (blockReason || enabling) return;
    setEnabling(true);
    try {
      const view = await enablePaymentChannel(channel.provider);
      if (!aliveRef.current) return;
      toast.success(`「${channel.name}」已启用`);
      replace(view);
      void reload();
    } catch {
      // 全局 toast 已弹；渠道状态以列表为准，保持在这一步
    } finally {
      if (aliveRef.current) setEnabling(false);
    }
  };

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4">
        <CopyBlock>
          <CopyBlockHeader>
            <CopyBlockTitle>回调地址：请在渠道商户后台登记这个地址</CopyBlockTitle>
            {channel.notify_url && <CopyButton text={channel.notify_url} />}
          </CopyBlockHeader>
          <CopyBlockCode className="break-all whitespace-pre-wrap">
            {channel.notify_url || "（未配置站点公网地址）"}
          </CopyBlockCode>
        </CopyBlock>
        {problem && <Notice tone="danger">{problem}</Notice>}
        {channel.enabled ? (
          <Notice tone="success">
            渠道已启用。前台要用它收款，还需要在全局设置里把微信 / 支付宝指给它。
          </Notice>
        ) : (
          !problem && (
            <Notice tone="info">
              启用后，还要在页面上方「全局设置」里把微信 / 支付宝指给它，前台才会使用这个渠道。
            </Notice>
          )
        )}
      </div>
      <SheetFooter className="flex-row justify-end border-t">
        <Button variant="outline" className="mr-auto" onClick={onBack}>
          上一步
        </Button>
        {channel.enabled ? (
          <Button onClick={onClose}>完成</Button>
        ) : (
          <ReasonTooltip reason={blockReason}>
            <Button disabled={!!blockReason || enabling} onClick={() => void enable()}>
              {enabling && <Loader2 className="animate-spin" />}
              启用渠道
            </Button>
          </ReasonTooltip>
        )}
      </SheetFooter>
    </>
  );
}
