import { Plus } from "lucide-react";
import { useState } from "react";

import type { ConfigIssue } from "@/api/admin/ai/type.d";
import type { Billing, Capabilities, PriceTier, Pricing } from "@/api/model/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
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
import { costValue, fallbackUnit } from "@/utils/admin/price-cost";

import { issueFor } from "./model-fields";
import {
  defaultUnit,
  PriceInput,
  priceField,
  billingBlocked,
  CostRow,
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

const NO_DIMENSION =
  "没有可区分价格的维度：在「能力与参数」里把清晰度、生成音频等参数设为规格价格维度";

/**
 * 积分定价页签：计费方式、适用范围（统一价格 / 按规格定价）；统一价格是一张默认价格卡，
 * 按规格定价是若干张规格价格卡（各有自己的价格与积分成本），不展示默认价格，
 * 默认价在后台同步为最低的规格价，作为没命中任何规格时的兜底。
 * 所有控件读写正文里的 pricing；价格一律是整数积分。切换计费方式时规格价格的单位不同，所以一并清空；
 * 切回统一价格会清掉所有规格价格。
 */
export function ModelPriceForm({
  body,
  kind,
  caps,
  issues,
  warnings,
  onChange,
}: {
  body: Record<string, unknown>;
  kind: string;
  caps: Capabilities | undefined;
  issues: ConfigIssue[];
  warnings: ConfigIssue[];
  onChange: (mutate: BodyMutator) => void;
}) {
  const pricing: Pricing = readModelPricing(body) ?? defaultPricing(kind);
  const tiers = pricing.tiers ?? [];
  const dims = tierDimensions(caps);
  const cost = pricing.cost;
  const setPricing = (next: Pricing) => onChange((body) => withModelField(body, "pricing", next));
  const patch = (partial: Partial<Pricing>) => setPricing({ ...pricing, ...partial });
  const setTiers = (next: PriceTier[]) => {
    const base = fallbackUnit(next);
    patch({
      tiers: next.length ? next : undefined,
      ...(base === undefined || pricing.billing === "token"
        ? {}
        : { [priceField(pricing.billing)]: base }),
    });
  };
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
  const sameUnitCost = costValue(pricing.billing, cost);

  // 已有规格价格时一定是「按规格定价」；还没添加时由运营的选择决定
  const [wantSpec, setWantSpec] = useState(false);
  const specMode = !isToken && (tiers.length > 0 || wantSpec);
  const switchScope = (spec: boolean) => {
    if (spec === specMode) return;
    setWantSpec(spec);
    if (!spec) setTiers([]);
  };

  return (
    <FormSection>
      <FormSectionHeader>
        <FormSectionTitle>用户积分价格</FormSectionTitle>
      </FormSectionHeader>

      <div className="flex flex-col gap-4" id="model-pricing">
        {kind === "agent" && caps?.video && (
          <Notice tone="info">
            看视频的子请求按这里的 Token 价照常扣费、计入本轮已花积分，不算进上下文大小。
          </Notice>
        )}
        <div className="flex flex-wrap items-start gap-x-8 gap-y-3 rounded-xl border p-4">
          <FormField
            size="default"
            label="计费方式"
            required
            error={issueFor(issues, "pricing.billing")}
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
          {!isToken && (
            <FormField size="default" label="适用范围">
              <Segmented aria-label="适用范围" className="w-fit">
                <SegmentedItem active={!specMode} onClick={() => switchScope(false)}>
                  统一价格
                </SegmentedItem>
                <SegmentedItem
                  active={specMode}
                  disabled={dims.length === 0 && !specMode}
                  title={dims.length === 0 ? NO_DIMENSION : undefined}
                  onClick={() => switchScope(true)}
                >
                  按规格定价
                </SegmentedItem>
              </Segmented>
            </FormField>
          )}
          <p className="text-muted-foreground w-full text-xs">
            {isToken
              ? "按 Token 计费不支持规格价格。"
              : specMode
                ? "按生成方式、清晰度等规格分别定价，每个规格可以单独设置积分成本；没命中任何规格时按最低的规格价收取。"
                : "所有请求使用同一个价格；需要区分生成方式、清晰度等规格时，切到「按规格定价」。"}
          </p>
          {specMode && issueFor(issues, `pricing.${field}`) && (
            <p className="text-destructive w-full text-xs">
              {issueFor(issues, `pricing.${field}`)}
            </p>
          )}
        </div>

        {specMode && (
          <div className="flex flex-col gap-3">
            {tiers.map((tier, index) => (
              <TierCard
                key={index}
                index={index}
                tier={tier}
                dims={dims}
                billing={pricing.billing}
                error={issueFor(issues, `pricing.tiers[${index}]`)}
                onChange={(next) => setTiers(tiers.map((item, i) => (i === index ? next : item)))}
                onRemove={() => setTiers(tiers.filter((_, i) => i !== index))}
              />
            ))}
            <button
              type="button"
              disabled={dims.length === 0}
              title={dims.length === 0 ? NO_DIMENSION : undefined}
              className="text-muted-foreground hover:bg-accent/40 hover:text-foreground flex h-12 w-full items-center justify-center gap-2 rounded-xl border border-dashed text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
              onClick={() =>
                setTiers([...tiers, { on: true, when: {}, unit: defaultUnit(pricing) }])
              }
            >
              <Plus className="size-4" />
              新增规格价格
            </button>
            {tiers.length > 1 && (
              <p className="text-muted-foreground text-xs">
                同时满足多条时取条件最多的一条，条件数相同取排在前面的；组合价格（如「4K +
                开音频」）要单独写一条。
              </p>
            )}
          </div>
        )}

        {!specMode && (
          <div className="rounded-xl border">
            <div className="flex items-center gap-3 border-b px-4 py-3">
              <span className="bg-muted grid size-8 place-items-center rounded-md font-mono text-xs">
                01
              </span>
              <div>
                <div className="text-sm font-semibold">默认价格</div>
              </div>
              {!isToken && sameUnitCost !== undefined && (
                <MarginBadge className="ml-auto" price={defaultUnit(pricing)} cost={sameUnitCost} />
              )}
            </div>
            <div className="flex flex-col gap-4 p-4">
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
                      warning={side === "in" ? issueFor(warnings, "pricing") : undefined}
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
                  warning={issueFor(warnings, "pricing")}
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

              {isToken ? (
                <div className="bg-muted/30 rounded-lg border p-3">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">积分成本</span>
                    <Tag>仅管理员可见</Tag>
                    <Switch
                      className="ml-auto"
                      checked={!!cost?.on}
                      aria-label="积分成本"
                      onCheckedChange={(on) => patch({ cost: on ? { ...cost, on } : undefined })}
                    />
                  </div>
                  {cost?.on && (
                    <div className="mt-3 flex flex-wrap items-center gap-4">
                      {(["in", "out"] as const).map((side) => (
                        <PriceInput
                          key={side}
                          label={side === "in" ? "输入成本" : "输出成本"}
                          value={cost.token?.[side]}
                          unit={`${side === "in" ? "输入" : "输出"} ${UNIT_LABEL.token}`}
                          className="w-64"
                          onChange={(value) =>
                            patch({
                              cost: {
                                ...cost,
                                token: { in: 0, out: 0, ...cost.token, [side]: value },
                              },
                            })
                          }
                        />
                      ))}
                      {(["in", "out"] as const).map((side) => (
                        <MarginBadge
                          key={side}
                          price={pricing.token?.[side] ?? 0}
                          cost={cost.token?.[side] ?? 0}
                        />
                      ))}
                    </div>
                  )}
                </div>
              ) : (
                <CostRow
                  label="默认价格"
                  billing={pricing.billing}
                  cost={cost}
                  onChange={(next) => patch({ cost: next })}
                />
              )}
            </div>
          </div>
        )}
      </div>
    </FormSection>
  );
}
