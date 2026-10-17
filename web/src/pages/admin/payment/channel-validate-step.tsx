import { useState } from "react";
import { Loader2 } from "lucide-react";

import { validatePaymentChannel } from "@/api/admin/payment";
import type { PaymentChannel } from "@/api/admin/payment/type.d";
import { Notice } from "@/components/admin-ui/notice";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Button } from "@/components/ui/button";
import { SheetFooter } from "@/components/ui/sheet";
import { errorMessage } from "@/utils/admin/errors";
import { OPS_ONLY_REASON } from "@/utils/admin/payment/channel";
import { formatAbsolute } from "@/utils/admin/user-rules";

import { useAliveRef } from "../use-admin";

/** 本次校验的结果：通过（可能没做在线探测）或失败（附原因） */
type ValidateResult = { kind: "ok"; probeSkipped: boolean } | { kind: "fail"; message: string };

/**
 * 配置抽屉第 2 步「校验」：本地格式校验 + 用一个不存在的订单号在线探测凭证。
 * 失败原因（65009）的全局 toast 照常弹出，这里把原因再就地显示成红色提示，不用盯着一闪而过的 toast；
 * 渠道无法在线探测时（probe_skipped）提示启用后先用小额真实订单测试。
 * @param channel 渠道视图（列表里的最新一份）
 * @param canWrite 是否有写权限（super_admin）
 * @param revalidateNotice 保存配置后的提示，如「配置已改动，需要重新校验」；没有为 null
 * @param onBack 回到第 1 步
 * @param onNext 进入第 3 步（渠道已通过校验才可用）
 * @param replace 用返回的渠道视图就地替换列表项
 */
export function ChannelValidateStep({
  channel,
  canWrite,
  revalidateNotice,
  onBack,
  onNext,
  replace,
}: {
  channel: PaymentChannel;
  canWrite: boolean;
  revalidateNotice: string | null;
  onBack: () => void;
  onNext: () => void;
  replace: (view: PaymentChannel) => void;
}) {
  const aliveRef = useAliveRef();
  const [validating, setValidating] = useState(false);
  const [result, setResult] = useState<ValidateResult | null>(null);

  const validate = async () => {
    if (!canWrite || validating) return;
    setValidating(true);
    setResult(null);
    try {
      const view = await validatePaymentChannel(channel.provider);
      if (!aliveRef.current) return;
      replace(view);
      setResult({ kind: "ok", probeSkipped: view.probe_skipped });
    } catch (error) {
      // 全局 toast 已弹，这里把原因翻译成面板内的就地提示
      if (aliveRef.current) setResult({ kind: "fail", message: errorMessage(error, "校验失败") });
    } finally {
      if (aliveRef.current) setValidating(false);
    }
  };

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4">
        {revalidateNotice && <Notice tone="warning">{revalidateNotice}</Notice>}
        <div className="bg-muted/40 rounded-lg border p-3 text-sm">
          <b>校验做两件事</b>
          <ul className="text-muted-foreground mt-1.5 list-disc space-y-1 pl-5">
            <li>本地格式校验：私钥能不能解析、网址是否合法、必填项是否齐全</li>
            <li>在线探测：用一个不存在的订单号查单，渠道返回「订单不存在」就说明凭证可用</li>
          </ul>
        </div>
        {validating && (
          <div className="text-muted-foreground flex items-center gap-2 text-sm" role="status">
            <Loader2 className="size-4 animate-spin" />
            校验中…
          </div>
        )}
        {result?.kind === "ok" && (
          <>
            <Notice tone="success">校验通过：配置格式正确，凭证可用。</Notice>
            {result.probeSkipped && (
              <Notice tone="warning">该渠道无法在线校验，启用后请先用小额真实订单测试。</Notice>
            )}
          </>
        )}
        {result?.kind === "fail" && (
          <Notice tone="danger" title="校验未通过">
            {result.message}
          </Notice>
        )}
        {!result && !validating && channel.validated && (
          <Notice tone="success">
            当前配置已于 {formatAbsolute(channel.validated_at)} 通过校验，可直接进入下一步。
          </Notice>
        )}
      </div>
      <SheetFooter className="flex-row justify-end border-t">
        <Button variant="outline" className="mr-auto" onClick={onBack}>
          上一步
        </Button>
        <ReasonTooltip reason={canWrite ? null : OPS_ONLY_REASON}>
          <Button
            variant={channel.validated ? "outline" : "default"}
            disabled={!canWrite || validating}
            onClick={() => void validate()}
          >
            {validating && <Loader2 className="animate-spin" />}
            {channel.validated ? "重新校验" : "开始校验"}
          </Button>
        </ReasonTooltip>
        <ReasonTooltip reason={channel.validated ? null : "请先通过校验"}>
          <Button disabled={!channel.validated || validating} onClick={onNext}>
            下一步
          </Button>
        </ReasonTooltip>
      </SheetFooter>
    </>
  );
}
