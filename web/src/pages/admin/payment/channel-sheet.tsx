import { useState } from "react";

import type { PaymentChannel } from "@/api/admin/payment/type.d";
import { Stepper, StepperItem } from "@/components/admin-ui/stepper";
import { Tag } from "@/components/admin-ui/tag";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { channelStatus } from "@/utils/admin/payment/channel";
import { channelToForm, type ChannelFormValues } from "@/utils/admin/payment/channel-form";

import { ChannelEnableStep } from "./channel-enable-step";
import { ChannelFillStep, type FillSaved } from "./channel-fill-step";
import { ChannelValidateStep } from "./channel-validate-step";

/** 配置步骤：1 填写、2 校验、3 启用 */
type Step = 1 | 2 | 3;

const STEP_LABELS = ["填写", "校验", "启用"] as const;

/**
 * 外壳里按步骤切换内容。表单取值由这一层持有，来回切步骤不丢；
 * 抽屉每次打开（或换渠道）会重新挂载，状态整份重置。
 */
function ChannelSheetBody({
  channel,
  canWrite,
  onClose,
  reload,
  replace,
}: {
  channel: PaymentChannel;
  canWrite: boolean;
  onClose: () => void;
  reload: () => Promise<PaymentChannel[] | null>;
  replace: (view: PaymentChannel) => void;
}) {
  const [step, setStep] = useState<Step>(1);
  const [values, setValues] = useState<ChannelFormValues>(() => channelToForm(channel));
  const [revalidateNotice, setRevalidateNotice] = useState<string | null>(null);
  const state = channelStatus(channel);

  const onSaved = ({ invalidated, wasEnabled }: FillSaved) => {
    setRevalidateNotice(
      invalidated
        ? wasEnabled
          ? "配置已改动，渠道已自动停用，需要重新校验后才能再启用。"
          : "配置已改动，需要重新校验。"
        : null,
    );
    setStep(2);
  };

  return (
    <>
      <SheetHeader className="gap-1.5">
        <SheetTitle>
          {canWrite ? "配置" : "查看"}「{channel.name}」
        </SheetTitle>
        <SheetDescription className="flex items-center gap-2">
          <span className="font-mono text-xs">{channel.provider}</span>
          <Tag tone={state.tone}>{state.label}</Tag>
        </SheetDescription>
      </SheetHeader>
      <Stepper className="px-4 pb-4" aria-label="配置步骤">
        {STEP_LABELS.map((label, index) => (
          <StepperItem
            key={label}
            index={index + 1}
            state={index + 1 < step ? "done" : index + 1 === step ? "current" : "todo"}
          >
            {label}
          </StepperItem>
        ))}
      </Stepper>
      {step === 1 && (
        <ChannelFillStep
          channel={channel}
          values={values}
          setValues={setValues}
          canWrite={canWrite}
          onClose={onClose}
          onNext={() => setStep(2)}
          onSaved={onSaved}
          reload={reload}
          replace={replace}
        />
      )}
      {step === 2 && (
        <ChannelValidateStep
          channel={channel}
          canWrite={canWrite}
          revalidateNotice={revalidateNotice}
          onBack={() => setStep(1)}
          onNext={() => setStep(3)}
          replace={replace}
        />
      )}
      {step === 3 && (
        <ChannelEnableStep
          channel={channel}
          canWrite={canWrite}
          onBack={() => setStep(2)}
          onClose={onClose}
          reload={reload}
          replace={replace}
        />
      )}
    </>
  );
}

/**
 * 渠道配置抽屉：顶部「1 填写 → 2 校验 → 3 启用」三步。
 * 渠道视图用列表里的最新一份，所以版本冲突或保存后列表刷新，抽屉里的 version 自然跟着更新，抽屉保持打开。
 * @param channel 当前渠道；关闭动画期间保持上一次的渠道，避免内容先消失
 * @param open 是否打开
 * @param canWrite 是否有写权限（super_admin）；admin 只能查看
 * @param onClose 关闭
 * @param reload 重新拉取渠道列表
 * @param replace 用返回的渠道视图就地替换列表项
 */
export function ChannelSheet({
  channel,
  open,
  canWrite,
  onClose,
  reload,
  replace,
}: {
  channel: PaymentChannel | null;
  open: boolean;
  canWrite: boolean;
  onClose: () => void;
  reload: () => Promise<PaymentChannel[] | null>;
  replace: (view: PaymentChannel) => void;
}) {
  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent className="gap-0 data-[side=right]:sm:max-w-[520px]">
        {channel && (
          <ChannelSheetBody
            key={channel.provider}
            channel={channel}
            canWrite={canWrite}
            onClose={onClose}
            reload={reload}
            replace={replace}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}
