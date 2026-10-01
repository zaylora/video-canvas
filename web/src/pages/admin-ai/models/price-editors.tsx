import { Trash2 } from "lucide-react";

import type {
  Billing,
  Capabilities,
  GenerationOp,
  ParamField,
  PriceTier,
  Pricing,
} from "@/api/model/type";
import { MarginBadge } from "@/components/admin-ui/margin-badge";
import { Tag } from "@/components/admin-ui/tag";
import { ToggleChip } from "@/components/admin-ui/toggle-chip";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { OP_LABEL, paramEntries } from "@/utils/tasks/capabilities";

/** 价格的固定范围（和后端 maxPrice 一致） */
export const PRICE_MAX = 1_000_000;

/** 计费方式的单位文案 */
export const UNIT_LABEL: Record<Billing, string> = {
  per_call: "积分/次",
  per_second: "积分/秒",
  token: "积分/百万 Token",
};

/** 整数积分输入框，带单位后缀；空输入当 0 */
export function PriceInput({
  id,
  value,
  unit,
  label,
  invalid,
  className,
  onChange,
}: {
  id?: string;
  value: number | undefined;
  unit: string;
  label: string;
  invalid?: boolean;
  className?: string;
  onChange: (value: number) => void;
}) {
  const out = value !== undefined && (value < 0 || value > PRICE_MAX || !Number.isInteger(value));
  return (
    <div className={cn("relative w-44", className)}>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        min={0}
        step={1}
        aria-label={label}
        aria-invalid={invalid || out}
        className="pr-24 tabular-nums"
        value={value === undefined ? "" : String(value)}
        onChange={(event) => onChange(Math.trunc(Number(event.target.value)) || 0)}
      />
      <span className="text-muted-foreground pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs">
        {unit}
      </span>
    </div>
  );
}

/** 一个规格价格维度：spec 参数、生成方式或「有参考视频」，以及它的可选取值 */
export type TierDimension = {
  key: string;
  label: string;
  values: Array<{ value: string | number | boolean; text: string }>;
};

/** 当前能力下可用的规格价格维度（spec 参数、生成方式多于一种时的 op、开启了参考视频时的 ref_video） */
export function tierDimensions(caps: Capabilities | undefined): TierDimension[] {
  const out: TierDimension[] = paramEntries(caps)
    .filter((field) => field.spec && (field.type === "enum" || field.type === "boolean"))
    .map((field) => ({
      key: field.name,
      label: field.label,
      values: specValues(field),
    }));
  if ((caps?.ops?.length ?? 0) > 1)
    out.push({
      key: "op",
      label: "生成方式",
      values: (caps?.ops ?? []).map((op: GenerationOp) => ({ value: op, text: OP_LABEL[op] })),
    });
  if (caps?.refs?.video?.on)
    out.push({
      key: "ref_video",
      label: "参考素材里有视频",
      values: [
        { value: true, text: "有" },
        { value: false, text: "没有" },
      ],
    });
  return out;
}

function specValues(field: ParamField) {
  if (field.type === "boolean")
    return [
      { value: true, text: "开启" },
      { value: false, text: "关闭" },
    ];
  return (field.options ?? []).map((option) => ({
    value: option,
    text: `${option}${field.unit ?? ""}`,
  }));
}

/** 条件里引用了已经不存在的维度或取值：返回给运营看的原因，合法返回 null */
export function staleCondition(tier: PriceTier, dims: TierDimension[]): string | null {
  for (const [key, value] of Object.entries(tier.when ?? {})) {
    const dim = dims.find((item) => item.key === key);
    if (!dim) return `条件里的「${key}」已不能作为规格维度`;
    if (!dim.values.some((item) => String(item.value) === String(value)))
      return `条件里的 ${String(value)} 已不可选`;
  }
  return null;
}

/** 一条规格价格：每个维度一行条件 chip（含「任意」）、价格、可供用户使用开关、利润率、删除 */
export function TierCard({
  index,
  tier,
  dims,
  billing,
  cost,
  error,
  onChange,
  onRemove,
}: {
  index: number;
  tier: PriceTier;
  dims: TierDimension[];
  billing: Billing;
  /** 成本（同单位），没开成本为 undefined */
  cost?: number;
  error?: string;
  onChange: (next: PriceTier) => void;
  onRemove: () => void;
}) {
  const stale = staleCondition(tier, dims);
  const empty = Object.keys(tier.when ?? {}).length === 0;
  const setCond = (key: string, value: string | number | boolean | undefined) => {
    const when = { ...tier.when };
    if (value === undefined) delete when[key];
    else when[key] = value;
    onChange({ ...tier, when });
  };
  const problem = error ?? stale ?? (empty ? "至少选一个条件，否则会盖掉默认价" : null);
  return (
    <div
      className={cn(
        "rounded-xl border",
        problem && "border-destructive/50",
        !tier.on && "opacity-60",
      )}
    >
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
        <span className="bg-muted grid size-7 place-items-center rounded-md font-mono text-xs">
          {String(index + 2).padStart(2, "0")}
        </span>
        <span className="text-sm font-semibold">规格价格</span>
        {cost !== undefined && <MarginBadge price={tier.unit} cost={cost} />}
        <label className="ml-auto flex items-center gap-1.5 text-xs">
          可供用户使用
          <Switch
            size="sm"
            checked={tier.on}
            aria-label={`第 ${index + 1} 条规格价格可供用户使用`}
            onCheckedChange={(on) => onChange({ ...tier, on })}
          />
        </label>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={`删除第 ${index + 1} 条规格价格`}
          onClick={onRemove}
        >
          <Trash2 />
        </Button>
      </div>
      <div className="flex flex-col gap-3 p-4">
        {dims.map((dim) => {
          const current = tier.when?.[dim.key];
          return (
            <div key={dim.key} className="flex flex-wrap items-center gap-1.5">
              <span className="text-muted-foreground w-28 shrink-0 text-xs">{dim.label}</span>
              <ToggleChip
                pressed={current === undefined}
                onClick={() => setCond(dim.key, undefined)}
              >
                任意
              </ToggleChip>
              {dim.values.map((item) => (
                <ToggleChip
                  key={String(item.value)}
                  pressed={current !== undefined && String(current) === String(item.value)}
                  onClick={() => setCond(dim.key, item.value)}
                >
                  {item.text}
                </ToggleChip>
              ))}
            </div>
          );
        })}
        {/* 条件里有、但维度已经不在了的键也列出来，方便运营删掉 */}
        {Object.keys(tier.when ?? {})
          .filter((key) => !dims.some((dim) => dim.key === key))
          .map((key) => (
            <div key={key} className="flex items-center gap-1.5">
              <span className="text-destructive w-28 shrink-0 text-xs">{key}</span>
              <Tag tone="danger">{String(tier.when[key])}</Tag>
              <Button size="xs" variant="ghost" onClick={() => setCond(key, undefined)}>
                移除条件
              </Button>
            </div>
          ))}
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground w-28 shrink-0 text-xs">价格</span>
          <PriceInput
            label={`第 ${index + 1} 条规格价格`}
            value={tier.unit}
            unit={UNIT_LABEL[billing]}
            onChange={(unit) => onChange({ ...tier, unit })}
          />
        </div>
        {problem && <p className="text-destructive text-xs">{problem}</p>}
      </div>
    </div>
  );
}

/** 默认价格所在的字段：按次 unit、按秒 per_second */
export const priceField = (billing: Billing) => (billing === "per_second" ? "per_second" : "unit");

/** 默认价（按次 / 按秒） */
export const defaultUnit = (pricing: Pricing) =>
  pricing.billing === "per_second" ? (pricing.per_second ?? 0) : (pricing.unit ?? 0);
