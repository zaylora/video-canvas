import { useState, type Dispatch, type SetStateAction } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { updatePaymentChannel } from "@/api/admin/payment";
import type { PaymentChannel, PaymentField } from "@/api/admin/payment/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { LockHint } from "@/components/admin-ui/lock-hint";
import { Notice } from "@/components/admin-ui/notice";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SheetFooter } from "@/components/ui/sheet";
import { OPS_ONLY_REASON } from "@/utils/admin/payment/channel";
import {
  TIMEOUT_RANGE,
  buildChannelRequest,
  channelToForm,
  fieldPlaceholder,
  invalidatesValidation,
  isChannelFormDirty,
  isFieldLocked,
  secretPlaceholder,
  validateTimeout,
  type ChannelFormValues,
} from "@/utils/admin/payment/channel-form";
import { isPaymentVersionConflict } from "@/utils/admin/payment/errors";

import { ReadOnlyNotice } from "../shared";
import { useAliveRef } from "../use-admin";

/** 保存成功后告诉外壳：要不要在第 2 步提示“配置已改动，需要重新校验” */
export type FillSaved = {
  /** 这次保存改了会让校验作废的内容（普通字段或密钥），渠道需要重新校验 */
  invalidated: boolean;
  /** 保存前渠道是启用状态：保存后已被后端自动停用 */
  wasEnabled: boolean;
};

/**
 * 配置抽屉第 1 步「填写」：按后端给的 fields 渲染表单。
 * 普通字段显示当前值；密钥字段不回显，留空表示不修改；有未完成订单时商户身份和密钥整体禁用（后端同样会以 65010 拒绝）。
 * 保存 = 整份非敏感字段 + 本次填写的密钥 + 打开抽屉时读到的 version；版本冲突（65011）的全局 toast 之后，
 * 这里重新拉取渠道并把表单刷新为最新内容，抽屉保持打开，避免静默覆盖别人的改动。
 * @param channel 渠道视图（列表里的最新一份，version 随刷新变化）
 * @param values 表单取值（由外壳持有，切步骤不丢）
 * @param setValues 更新表单取值
 * @param canWrite 是否有写权限（super_admin）
 * @param onClose 取消 / 关闭抽屉
 * @param onNext 已保存且无改动时直接进入下一步
 * @param onSaved 保存成功
 * @param reload 重新拉取渠道列表，返回最新列表（失败为 null）
 * @param replace 用返回的渠道视图就地替换列表项
 */
export function ChannelFillStep({
  channel,
  values,
  setValues,
  canWrite,
  onClose,
  onNext,
  onSaved,
  reload,
  replace,
}: {
  channel: PaymentChannel;
  values: ChannelFormValues;
  setValues: Dispatch<SetStateAction<ChannelFormValues>>;
  canWrite: boolean;
  onClose: () => void;
  onNext: () => void;
  onSaved: (saved: FillSaved) => void;
  reload: () => Promise<PaymentChannel[] | null>;
  replace: (view: PaymentChannel) => void;
}) {
  const aliveRef = useAliveRef();
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const locked = channel.open_orders > 0;
  const timeoutError = validateTimeout(values.timeout);
  const dirty = isChannelFormDirty(channel, values);
  const willInvalidate = invalidatesValidation(channel, values);
  const inputDisabled = !canWrite || saving;

  const save = async () => {
    if (!canWrite || saving || timeoutError || !dirty) return;
    const saved: FillSaved = { invalidated: willInvalidate, wasEnabled: channel.enabled };
    setSaving(true);
    try {
      const view = await updatePaymentChannel(
        channel.provider,
        buildChannelRequest(channel, values),
      );
      if (!aliveRef.current) return;
      toast.success("配置已保存");
      replace(view);
      void reload();
      setValues(channelToForm(view));
      onSaved(saved);
    } catch (error) {
      // 全局 toast 已弹；版本冲突时用最新内容刷新表单，其余错误保持当前输入
      if (isPaymentVersionConflict(error)) {
        const fresh = (await reload())?.find((item) => item.provider === channel.provider);
        if (fresh && aliveRef.current) {
          setValues(channelToForm(fresh));
          setConflict(true);
        }
      }
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  const setConfig = (key: string, value: string) =>
    setValues((prev) => ({ ...prev, config: { ...prev.config, [key]: value } }));
  const setSecret = (key: string, value: string) =>
    setValues((prev) => ({ ...prev, secrets: { ...prev.secrets, [key]: value } }));

  const renderField = (field: PaymentField) => {
    const fieldLocked = isFieldLocked(channel, field);
    const showLock = field.identity || fieldLocked;
    const label = (
      <>
        {field.label}
        {showLock && (
          <LockHint
            reason={
              fieldLocked
                ? "该渠道有未完成订单，暂不能修改"
                : "商户身份字段：渠道有未完成订单时不能修改"
            }
          />
        )}
      </>
    );
    const id = `payment-field-${channel.provider}-${field.key}`;
    return (
      <FormField
        key={field.key}
        label={label}
        htmlFor={id}
        required={!field.optional}
        hint={field.optional ? "可不填" : undefined}
      >
        {field.secret ? (
          <Input
            id={id}
            type="password"
            autoComplete="new-password"
            placeholder={secretPlaceholder(field)}
            value={values.secrets[field.key] ?? ""}
            disabled={inputDisabled || fieldLocked}
            onChange={(event) => setSecret(field.key, event.target.value)}
          />
        ) : (
          <Input
            id={id}
            autoComplete="off"
            placeholder={fieldPlaceholder(field)}
            value={values.config[field.key] ?? ""}
            disabled={inputDisabled || fieldLocked}
            onChange={(event) => setConfig(field.key, event.target.value)}
          />
        )}
      </FormField>
    );
  };

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4">
        {!canWrite && <ReadOnlyNotice what="修改渠道配置" />}
        {locked && (
          <Notice tone="warning">
            该渠道有 {channel.open_orders} 笔未完成订单，暂不能修改商户身份和密钥。
          </Notice>
        )}
        {conflict && (
          <Notice tone="warning">配置已被他人修改，表单已刷新为最新内容，请核对后再保存。</Notice>
        )}
        {channel.fields.map(renderField)}
        <FormField
          label="未支付自动关闭（分钟）"
          htmlFor={`payment-timeout-${channel.provider}`}
          error={timeoutError ?? undefined}
          hint={`${TIMEOUT_RANGE.min}–${TIMEOUT_RANGE.max} 分钟；只改这一项不会让渠道停用，也不用重新校验。`}
          className="sm:max-w-48"
        >
          <Input
            id={`payment-timeout-${channel.provider}`}
            inputMode="numeric"
            autoComplete="off"
            className="tabular-nums"
            value={values.timeout}
            disabled={inputDisabled}
            aria-invalid={!!timeoutError}
            onChange={(event) => setValues((prev) => ({ ...prev, timeout: event.target.value }))}
          />
        </FormField>
        {canWrite && willInvalidate && (
          <Notice tone="warning">
            {channel.enabled
              ? "保存后该渠道会自动停用，并需要重新校验后才能再启用。"
              : "保存后需要重新校验。"}
          </Notice>
        )}
      </div>
      <SheetFooter className="flex-row justify-end border-t">
        <Button variant="outline" onClick={onClose}>
          {canWrite ? "取消" : "关闭"}
        </Button>
        {channel.configured && !dirty ? (
          <Button onClick={onNext}>下一步</Button>
        ) : (
          <ReasonTooltip reason={canWrite ? null : OPS_ONLY_REASON}>
            <Button
              disabled={!canWrite || saving || !!timeoutError || !dirty}
              onClick={() => void save()}
            >
              {saving && <Loader2 className="animate-spin" />}
              保存并继续
            </Button>
          </ReasonTooltip>
        )}
      </SheetFooter>
    </>
  );
}
