import { useCallback, useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { updatePricingSettings } from "@/api/admin/settings";
import type { PricingSettingsView } from "@/api/admin/settings/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { GLASS_PANEL } from "@/components/admin-ui/glass";
import { useAdminConfigChanged } from "@/hooks/use-admin-config-changed";
import {
  canSavePricing,
  describeUsdCny,
  pricingFromView,
  pricingToRequest,
  validatePricing,
  type PricingValues,
} from "@/utils/admin/pricing-rules";

import { useAliveRef } from "../use-admin";
import { SettingsError, SettingsSkeleton } from "./settings-state";
import { usePricingSettings } from "./use-settings";

/**
 * 当前生效的美元汇率：汇率、来源、日期，实时值过期时标出来。
 * 汇率由系统写入或由下面的覆盖框决定，这里只读
 * @param usd 服务端给的汇率
 */
function UsdCnyLine({ usd }: { usd: PricingSettingsView["usd_cny"] }) {
  const view = describeUsdCny(usd);
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm">
      <span className="text-muted-foreground">当前汇率</span>
      <span className="font-mono font-medium tabular-nums">
        {view.rate ? `USD/CNY ${view.rate}` : "—"}
      </span>
      <Tag tone={usd.source === "none" ? "danger" : "neutral"}>{view.source}</Tag>
      {view.date && (
        <span className="text-muted-foreground font-mono text-xs tabular-nums">{view.date}</span>
      )}
      {view.stale && <Tag tone="warning">已过期</Tag>}
    </div>
  );
}

/**
 * 定价设置表单：1 元 = N 积分、默认加价率、美元汇率覆盖（可空，空表示用实时汇率）。
 * @param settings 已保存的设置
 * @param onSaved 保存成功后带回服务端返回的设置
 */
function PricingForm({
  settings,
  onSaved,
}: {
  settings: PricingSettingsView;
  onSaved: (next: PricingSettingsView) => void;
}) {
  const aliveRef = useAliveRef();
  const [initial, setInitial] = useState(() => pricingFromView(settings));
  const [values, setValues] = useState<PricingValues>(initial);
  const [saving, setSaving] = useState(false);
  const errors = validatePricing(values);
  const canSave = canSavePricing({ values, initial, saving });
  const set = (patch: Partial<PricingValues>) => setValues((prev) => ({ ...prev, ...patch }));

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      const next = await updatePricingSettings(pricingToRequest(values));
      if (!aliveRef.current) return;
      toast.success("定价设置已保存");
      const saved = pricingFromView(next);
      setInitial(saved);
      setValues(saved);
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
      <Card className={GLASS_PANEL}>
        <CardContent className="flex flex-col gap-5">
          <UsdCnyLine usd={settings.usd_cny} />
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              label="1 元 = N 积分"
              htmlFor="pricing-credits"
              error={errors.credits_per_yuan}
            >
              <Input
                id="pricing-credits"
                inputMode="decimal"
                value={values.credits_per_yuan}
                disabled={saving}
                aria-invalid={!!errors.credits_per_yuan}
                onChange={(event) => set({ credits_per_yuan: event.target.value })}
              />
            </FormField>
            <FormField
              label="默认加价率（%）"
              htmlFor="pricing-markup"
              error={errors.default_markup_pct}
            >
              <Input
                id="pricing-markup"
                inputMode="numeric"
                value={values.default_markup_pct}
                disabled={saving}
                aria-invalid={!!errors.default_markup_pct}
                onChange={(event) => set({ default_markup_pct: event.target.value })}
              />
            </FormField>
            <FormField label="美元汇率覆盖" htmlFor="pricing-fx" error={errors.usd_cny_override}>
              <Input
                id="pricing-fx"
                inputMode="decimal"
                placeholder="留空使用实时汇率"
                value={values.usd_cny_override}
                disabled={saving}
                aria-invalid={!!errors.usd_cny_override}
                onChange={(event) => set({ usd_cny_override: event.target.value })}
              />
            </FormField>
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button type="submit" disabled={!canSave}>
            {saving && <Loader2 className="animate-spin" />}
            保存
          </Button>
        </CardFooter>
      </Card>
    </form>
  );
}

/**
 * 定价设置页（/admin/settings/pricing）：admin 和 super_admin 都可以读写。
 * 后台助手应用了定价设置的变更单后，收到 admin-config-changed 重新拉取并重置表单。
 * 请求错误的全局提示由拦截器弹，这里不重复。
 */
export default function PricingSettingsPage() {
  const { data, status, reload, setData } = usePricingSettings();
  /** 外部改动后递增：让表单按最新数据重新初始化 */
  const [version, setVersion] = useState(0);
  const refresh = useCallback(() => {
    void reload().then(() => setVersion((n) => n + 1));
  }, [reload]);
  useAdminConfigChanged("pricing_settings", refresh);

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>定价设置</PageHeaderTitle>
          </PageHeaderHeading>
        </PageHeader>

        {status === "loading" && <SettingsSkeleton />}
        {status === "error" && <SettingsError onRetry={() => void reload()} />}
        {status === "ready" && data && (
          <PricingForm key={version} settings={data} onSaved={setData} />
        )}
      </main>
    </div>
  );
}
