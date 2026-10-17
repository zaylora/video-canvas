import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { getPaymentSettings, updatePaymentSettings } from "@/api/admin/payment";
import type { PaymentChannel, PaymentSettings } from "@/api/admin/payment/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { OPS_ONLY_REASON } from "@/utils/admin/payment/channel";
import {
  MAX_PENDING_RANGE,
  callbackPreview,
  isSettingsDirty,
  methodOptions,
  settingsFormToRequest,
  settingsToForm,
  validateSettingsForm,
} from "@/utils/admin/payment/settings";

import { useAliveRef, type LoadStatus } from "../use-admin";

/**
 * 全局设置表单：支付总开关、站点公网地址、两种支付方式各由哪个渠道承接、每人最多未支付订单数。
 * 保存失败（承接的渠道未启用 65009、没配公网地址 65013）的提示由全局 toast 弹，表单保持当前输入不动。
 * @param settings 已保存的设置
 * @param channels 渠道列表（承接下拉的选项来源）
 * @param canWrite 是否有写权限（super_admin）
 * @param onSaved 保存成功，带回服务端返回的设置
 */
function SettingsForm({
  settings,
  channels,
  canWrite,
  onSaved,
}: {
  settings: PaymentSettings;
  channels: PaymentChannel[];
  canWrite: boolean;
  onSaved: (next: PaymentSettings) => void;
}) {
  const aliveRef = useAliveRef();
  const [values, setValues] = useState(() => settingsToForm(settings));
  const [saving, setSaving] = useState(false);
  const errors = validateSettingsForm(values);
  const invalid = Object.keys(errors).length > 0;
  const dirty = isSettingsDirty(values, settings);
  const disabled = !canWrite || saving;
  const preview = callbackPreview(values.publicBaseUrl);

  const save = async () => {
    if (disabled || invalid || !dirty) return;
    setSaving(true);
    try {
      const next = await updatePaymentSettings(settingsFormToRequest(values));
      if (!aliveRef.current) return;
      toast.success("全局设置已保存");
      setValues(settingsToForm(next));
      onSaved(next);
    } catch {
      // 全局 toast 已弹，表单保留当前输入
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <CardContent className="flex flex-col gap-5">
        <div className="flex items-center justify-between gap-4">
          <div className="min-w-0">
            <label htmlFor="payment-enabled" className="text-sm font-medium">
              支付总开关
            </label>
            <p className="text-muted-foreground mt-0.5 text-xs">
              关闭后前台恢复成余额小卡片，下单返回「支付功能未开启」；渠道回调和补单照常运行。
            </p>
          </div>
          <Switch
            id="payment-enabled"
            checked={values.enabled}
            disabled={disabled}
            onCheckedChange={(checked) => setValues((prev) => ({ ...prev, enabled: checked }))}
          />
        </div>

        <FormField
          label="站点公网地址"
          htmlFor="payment-base-url"
          error={errors.publicBaseUrl}
          hint="用来拼渠道回调地址和支付后跳回的地址，如 https://example.com，不带路径。"
        >
          <Input
            id="payment-base-url"
            autoComplete="off"
            placeholder="https://example.com"
            value={values.publicBaseUrl}
            disabled={disabled}
            aria-invalid={!!errors.publicBaseUrl}
            onChange={(event) =>
              setValues((prev) => ({ ...prev, publicBaseUrl: event.target.value }))
            }
          />
        </FormField>

        <div className="grid gap-4 sm:grid-cols-2">
          <FormField
            label="微信由谁承接"
            htmlFor="payment-method-wechat"
            hint="只能选已启用的渠道；选「不开放」则前台微信支付不可用。"
          >
            <OptionSelect
              id="payment-method-wechat"
              value={values.methodWechat}
              options={methodOptions(channels, "wechat")}
              disabled={disabled}
              onValueChange={(value) => setValues((prev) => ({ ...prev, methodWechat: value }))}
            />
          </FormField>
          <FormField
            label="支付宝由谁承接"
            htmlFor="payment-method-alipay"
            hint="只能选已启用的渠道；选「不开放」则前台支付宝不可用。"
          >
            <OptionSelect
              id="payment-method-alipay"
              value={values.methodAlipay}
              options={methodOptions(channels, "alipay")}
              disabled={disabled}
              onValueChange={(value) => setValues((prev) => ({ ...prev, methodAlipay: value }))}
            />
          </FormField>
        </div>

        <FormField
          label="每人最多未支付订单"
          htmlFor="payment-max-pending"
          error={errors.maxPending}
          hint={`${MAX_PENDING_RANGE.min}–${MAX_PENDING_RANGE.max} 笔，超过后用户需要先完成或取消旧订单。`}
          className="sm:max-w-48"
        >
          <Input
            id="payment-max-pending"
            inputMode="numeric"
            autoComplete="off"
            className="tabular-nums"
            value={values.maxPending}
            disabled={disabled}
            aria-invalid={!!errors.maxPending}
            onChange={(event) => setValues((prev) => ({ ...prev, maxPending: event.target.value }))}
          />
        </FormField>

        <p className="text-muted-foreground font-mono text-xs break-all">
          回调地址：{preview ?? "未配置站点公网地址"}
        </p>
      </CardContent>
      <CardFooter className="justify-end border-t py-3">
        <ReasonTooltip reason={canWrite ? null : OPS_ONLY_REASON}>
          <Button type="submit" disabled={disabled || invalid || !dirty}>
            {saving && <Loader2 className="animate-spin" />}
            保存
          </Button>
        </ReasonTooltip>
      </CardFooter>
    </form>
  );
}

/**
 * 「全局设置」卡：进页面读一次设置；加载中显示骨架，失败可重试。
 * 保存和读取的请求错误由全局 toast 展示，这里只管状态。
 * @param channels 渠道列表（承接下拉的选项来源）
 * @param canWrite 是否有写权限（super_admin）；admin 只读，保存按钮禁用并说明原因
 * @param onSaved 保存成功后通知外面刷新渠道（承接关系变了，渠道的“承接”列要跟着变）
 */
export function SettingsCard({
  channels,
  canWrite,
  onSaved,
}: {
  channels: PaymentChannel[];
  canWrite: boolean;
  onSaved: () => void;
}) {
  const aliveRef = useAliveRef();
  const [settings, setSettings] = useState<PaymentSettings | null>(null);
  const [status, setStatus] = useState<LoadStatus>("loading");

  const load = useCallback(
    () =>
      getPaymentSettings().then(
        (next) => {
          if (!aliveRef.current) return;
          setSettings(next);
          setStatus("ready");
        },
        () => {
          // 全局 toast 已弹，卡片显示失败态和重试
          if (aliveRef.current) setStatus("error");
        },
      ),
    [aliveRef],
  );

  useEffect(() => {
    void load();
  }, [load]);

  /** 加载失败后的“重试”：先回到加载态，再请求 */
  const retry = () => {
    setStatus("loading");
    void load();
  };

  return (
    <Card data-slot="payment-settings-card">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          全局设置
          {!canWrite && <Tag>只读 · 仅运维可修改</Tag>}
        </CardTitle>
        <CardDescription>
          支付总开关、回调用的公网地址，以及微信 / 支付宝各由哪个渠道承接。
        </CardDescription>
      </CardHeader>
      {status === "loading" && (
        <CardContent>
          <Skeleton className="h-64 rounded-lg" />
        </CardContent>
      )}
      {status === "error" && (
        <CardContent className="flex items-center gap-3">
          <span className="text-destructive text-sm">加载失败</span>
          <Button variant="outline" size="sm" onClick={retry}>
            重试
          </Button>
        </CardContent>
      )}
      {status === "ready" && settings && (
        <SettingsForm
          settings={settings}
          channels={channels}
          canWrite={canWrite}
          onSaved={(next) => {
            setSettings(next);
            onSaved();
          }}
        />
      )}
    </Card>
  );
}
