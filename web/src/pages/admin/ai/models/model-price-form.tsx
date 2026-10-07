import { Plus } from "lucide-react";

import type { ConfigIssue } from "@/api/admin/ai/type.d";
import type { Billing, Capabilities, PriceCost, PriceTier, Pricing } from "@/api/model/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { MarginBadge } from "@/components/admin-ui/margin-badge";
import { Notice } from "@/components/admin-ui/notice";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Tag } from "@/components/admin-ui/tag";
import { Switch } from "@/components/ui/switch";
import { readModelPricing, withModelField } from "@/utils/admin/model-body";
import { defaultPricing } from "@/utils/admin/model-template";

import { issueFor } from "./model-fields";
import {
  defaultUnit,
  PriceInput,
  priceField,
  TierCard,
  tierDimensions,
  UNIT_LABEL,
} from "./price-editors";

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

const BILLING_LABEL: Record<Billing, string> = {
  per_call: "按次",
  per_second: "按秒",
  token: "Token",
};

/** 计费方式在当前模型下能不能用；不能用返回原因 */
function billingBlocked(billing: Billing, kind: string, caps: Capabilities | undefined) {
  if (billing === "per_second" && caps?.params?.duration?.type !== "number")
    return "按秒计费需要一个名为 duration 的数字参数（在「能力与参数」里添加视频时长）";
  if (kind === "agent" && billing !== "token") return "Agent 模型只能按 Token 计费";
  if (billing === "token" && kind !== "text" && kind !== "agent")
    return "只有文本、Agent 模型按 Token 计费";
  return null;
}

/** 成本里与默认价同单位的那个数 */
const costUnit = (pricing: Pricing, cost: PriceCost | undefined) =>
  !cost?.on
    ? undefined
    : pricing.billing === "per_second"
      ? (cost.per_second ?? 0)
      : (cost.unit ?? 0);

/**
 * 积分定价页签：默认价格（计费方式、价格、积分成本与利润率）+ 规格价格（每条一张卡）。
 * 所有控件读写正文里的 pricing；价格一律是整数积分。切换计费方式时规格价格的单位不同，所以一并清空。
 */
export function ModelPriceForm({
  body,
  kind,
  caps,
  issues,
  onChange,
}: {
  body: Record<string, unknown>;
  kind: string;
  caps: Capabilities | undefined;
  issues: ConfigIssue[];
  onChange: (mutate: BodyMutator) => void;
}) {
  const pricing: Pricing = readModelPricing(body) ?? defaultPricing(kind);
  const tiers = pricing.tiers ?? [];
  const dims = tierDimensions(caps);
  const cost = pricing.cost;
  const setPricing = (next: Pricing) => onChange((body) => withModelField(body, "pricing", next));
  const patch = (partial: Partial<Pricing>) => setPricing({ ...pricing, ...partial });
  const setTiers = (next: PriceTier[]) => patch({ tiers: next.length ? next : undefined });
  const switchBilling = (billing: Billing) => {
    if (billing === pricing.billing) return;
    setPricing({
      billing,
      ...(billing === "token" ? { token: { in: 0, out: 0 } } : { [priceField(billing)]: 0 }),
      ...(cost?.on ? { cost: { on: true } } : {}),
    });
  };
  const isToken = pricing.billing === "token";
  const field = priceField(pricing.billing);
  const sameUnitCost = costUnit(pricing, cost);

  return (
    <FormSection>
      <FormSectionHeader>
        <FormSectionTitle>用户积分价格</FormSectionTitle>
        <FormSectionDescription>
          价格都是整数积分。默认价格对所有请求生效；要按清晰度、音频、生成方式等区分时，再加规格价格。
        </FormSectionDescription>
      </FormSectionHeader>

      <div className="rounded-xl border" id="model-pricing">
        <div className="flex items-center gap-3 border-b px-4 py-3">
          <span className="bg-muted grid size-8 place-items-center rounded-md font-mono text-xs">
            01
          </span>
          <div>
            <div className="text-sm font-semibold">默认价格</div>
            <div className="text-muted-foreground text-xs">没有命中规格价格的请求都用这个价格</div>
          </div>
          {!isToken && sameUnitCost !== undefined && (
            <MarginBadge className="ml-auto" price={defaultUnit(pricing)} cost={sameUnitCost} />
          )}
        </div>
        <div className="flex flex-col gap-4 p-4">
          <FormField
            size="default"
            label="计费方式"
            required
            error={issueFor(issues, "pricing.billing")}
            hint={
              pricing.billing === "per_second"
                ? "每秒价 × 用户拖动的时长；切换计费方式会清空规格价格。"
                : isToken
                  ? "按上限预冻结，完成后按实际用量结算，多冻结的退回。"
                  : "每生成一次扣一次；切换计费方式会清空规格价格。"
            }
          >
            <Segmented aria-label="计费方式" className="w-fit">
              {(Object.keys(BILLING_LABEL) as Billing[]).map((billing) => {
                const reason = billingBlocked(billing, kind, caps);
                return (
                  <SegmentedItem
                    key={billing}
                    active={billing === pricing.billing}
                    disabled={!!reason && billing !== pricing.billing}
                    title={reason ?? undefined}
                    onClick={() => switchBilling(billing)}
                  >
                    {BILLING_LABEL[billing]}
                  </SegmentedItem>
                );
              })}
            </Segmented>
          </FormField>

          {isToken ? (
            <div className="flex flex-wrap gap-5">
              {(["in", "out"] as const).map((side) => (
                <FormField
                  key={side}
                  size="default"
                  label={side === "in" ? "输入价" : "输出价"}
                  required
                  error={
                    issueFor(issues, `pricing.token.${side}`) ??
                    (side === "in" ? issueFor(issues, "pricing.token") : undefined)
                  }
                >
                  <PriceInput
                    label={side === "in" ? "输入价" : "输出价"}
                    value={pricing.token?.[side]}
                    unit={UNIT_LABEL.token}
                    className="w-56"
                    onChange={(value) =>
                      patch({ token: { in: 0, out: 0, ...pricing.token, [side]: value } })
                    }
                  />
                </FormField>
              ))}
            </div>
          ) : (
            <FormField
              size="default"
              label={pricing.billing === "per_second" ? "每秒消耗积分" : "每次消耗积分"}
              htmlFor="model-price"
              required
              error={issueFor(issues, `pricing.${field}`)}
            >
              <PriceInput
                id="model-price"
                label="默认价格"
                value={defaultUnit(pricing)}
                unit={UNIT_LABEL[pricing.billing]}
                invalid={!!issueFor(issues, `pricing.${field}`)}
                onChange={(value) => patch({ [field]: value })}
              />
            </FormField>
          )}

          <div className="bg-muted/30 rounded-lg border p-3">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium">积分成本</span>
              <Tag>仅管理员可见</Tag>
              <span className="text-muted-foreground hidden text-xs sm:inline">
                填了成本才能看到利润率，不影响用户价格，也不会下发给画布。
              </span>
              <Switch
                className="ml-auto"
                checked={!!cost?.on}
                aria-label="积分成本"
                onCheckedChange={(on) => patch({ cost: on ? { ...cost, on } : undefined })}
              />
            </div>
            {cost?.on && (
              <div className="mt-3 flex flex-wrap items-center gap-4">
                {isToken ? (
                  (["in", "out"] as const).map((side) => (
                    <PriceInput
                      key={side}
                      label={side === "in" ? "输入成本" : "输出成本"}
                      value={cost.token?.[side]}
                      unit={`${side === "in" ? "输入" : "输出"} ${UNIT_LABEL.token}`}
                      className="w-64"
                      onChange={(value) =>
                        patch({
                          cost: { ...cost, token: { in: 0, out: 0, ...cost.token, [side]: value } },
                        })
                      }
                    />
                  ))
                ) : (
                  <PriceInput
                    label="积分成本"
                    value={sameUnitCost}
                    unit={UNIT_LABEL[pricing.billing]}
                    onChange={(value) => patch({ cost: { ...cost, [field]: value } })}
                  />
                )}
                {isToken &&
                  (["in", "out"] as const).map((side) => (
                    <MarginBadge
                      key={side}
                      price={pricing.token?.[side] ?? 0}
                      cost={cost.token?.[side] ?? 0}
                    />
                  ))}
              </div>
            )}
          </div>
        </div>
      </div>

      {isToken ? (
        <Notice tone="info" className="mt-4">
          按 Token 计费不需要规格价格：费用按输入、输出 Token 数自动计算。
        </Notice>
      ) : (
        <>
          <div className="mt-4 flex flex-col gap-3">
            {tiers.map((tier, index) => (
              <TierCard
                key={index}
                index={index}
                tier={tier}
                dims={dims}
                billing={pricing.billing}
                cost={sameUnitCost}
                error={issueFor(issues, `pricing.tiers[${index}]`)}
                onChange={(next) => setTiers(tiers.map((item, i) => (i === index ? next : item)))}
                onRemove={() => setTiers(tiers.filter((_, i) => i !== index))}
              />
            ))}
          </div>
          <button
            type="button"
            disabled={dims.length === 0}
            title={
              dims.length === 0
                ? "没有可区分价格的维度：在「能力与参数」里把清晰度、生成音频等参数设为规格价格维度"
                : undefined
            }
            className="text-muted-foreground hover:bg-accent/40 hover:text-foreground mt-3 flex h-12 w-full items-center justify-center gap-2 rounded-xl border border-dashed text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
            onClick={() => setTiers([...tiers, { on: true, when: {}, unit: defaultUnit(pricing) }])}
          >
            <Plus className="size-4" />
            新增规格价格
          </button>
          {tiers.length > 1 && (
            <p className="text-muted-foreground mt-2 text-xs">
              同时满足多条时取条件最多的一条，条件数相同取排在前面的；组合价格（如「4K +
              开音频」）要单独写一条。
            </p>
          )}
        </>
      )}
    </FormSection>
  );
}
