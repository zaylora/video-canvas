import type { ReactNode } from "react";
import { Eye, Star } from "lucide-react";

import type { ConfigListItem } from "@/api/admin-ai/type.d";
import type { Capabilities, Pricing } from "@/api/model/type.d";
import { Tag } from "@/components/admin-ui/tag";
import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { matchTier, quote, type PriceSpec } from "@/utils/pricing/quote";
import { paramEntries } from "@/utils/tasks/capabilities";

/** 右侧预览栏的小标题 + 底部说明 */
export function PreviewFrame({
  title,
  note,
  children,
}: {
  title: string;
  note?: ReactNode;
  children: ReactNode;
}) {
  return (
    <>
      <div className="text-muted-foreground mb-3 flex items-center gap-2 text-xs font-medium">
        <Eye className="size-3.5" />
        {title}
      </div>
      {children}
      {note && <p className="text-muted-foreground mt-3 text-xs leading-relaxed">{note}</p>}
    </>
  );
}

function PickerRow({
  name,
  seed,
  vendor,
  tags,
  price,
  hint,
  active,
}: {
  name: string;
  seed: string;
  vendor?: string;
  tags?: readonly string[];
  /** 价格文案，如「10 积分」「2 积分/秒起」 */
  price?: string;
  hint?: string;
  active?: boolean;
}) {
  return (
    <div
      className={cn(
        "flex gap-3 rounded-lg p-2.5",
        active ? "bg-accent ring-foreground/20 ring-1" : "opacity-45",
      )}
    >
      <VendorAvatar vendor={vendor} name={name} seed={seed} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm font-medium">{name || "未命名模型"}</span>
          {tags?.map((tag) => (
            <Tag key={tag} tone="info" className="px-1 py-0 text-[10px]">
              {tag}
            </Tag>
          ))}
          {price && (
            <span className="text-muted-foreground ml-auto shrink-0 text-xs tabular-nums">
              {price}
            </span>
          )}
        </div>
        {hint && <p className="text-muted-foreground mt-1 line-clamp-2 text-xs">{hint}</p>}
      </div>
    </div>
  );
}

/** 基本信息页签的预览：创作端的模型选择器，高亮当前模型，灰色是同类已上架的模型 */
export function PickerPreview({
  modelKey,
  label,
  kind,
  vendor,
  tags,
  price,
  hint,
  models,
}: {
  modelKey: string;
  label: string;
  kind: string;
  vendor: string;
  tags: readonly string[];
  price: string;
  hint: string;
  models: ConfigListItem[];
}) {
  const others = models
    .filter((item) => item.kind === kind && item.key !== modelKey && item.enabled)
    .slice(0, 2);
  return (
    <PreviewFrame
      title="创作端预览 · 模型选择器"
      note="高亮的是当前正在编辑的模型，灰色是同类已上架的模型，方便对照名称是否清楚。"
    >
      <div className="bg-popover rounded-xl border p-2 shadow-lg">
        <div className="text-muted-foreground px-2 pt-1 pb-2 text-xs">
          {MODEL_KIND_LABEL[kind] ?? kind}模型
        </div>
        <PickerRow
          active
          name={label}
          seed={modelKey}
          vendor={vendor}
          tags={tags}
          price={price}
          hint={hint}
        />
        {others.map((item) => (
          <PickerRow
            key={item.key}
            name={item.label || item.name || item.key}
            seed={item.key}
            vendor={item.vendor}
            tags={item.tags}
          />
        ))}
      </div>
    </PreviewFrame>
  );
}

/** 价格预览里的一个维度：spec 参数（enum / boolean）的全部取值 */
type PreviewDim = { name: string; label: string; values: Array<{ value: unknown; text: string }> };

function previewDims(caps: Capabilities | undefined): PreviewDim[] {
  return paramEntries(caps)
    .filter((field) => field.spec && (field.type === "enum" || field.type === "boolean"))
    .map((field) => ({
      name: field.name,
      label: field.label,
      values:
        field.type === "boolean"
          ? [
              { value: true, text: "开" },
              { value: false, text: "关" },
            ]
          : (field.options ?? []).map((option) => ({ value: option, text: String(option) })),
    }));
}

/** 所有参数取默认值的规格（生成方式取第一种），预览时再覆盖要变化的那几个 */
function defaultSpec(caps: Capabilities | undefined): PriceSpec {
  const params: Record<string, unknown> = {};
  for (const field of paramEntries(caps)) params[field.name] = field.default;
  return { op: caps?.ops?.[0], refVideo: false, params, promptChars: 0 };
}

/** 一个格子的价格：命中规格价时蓝色，等于默认参数时加星 */
function Cell({
  pricing,
  caps,
  spec,
  star,
  suffix,
}: {
  pricing: Pricing;
  caps: Capabilities | undefined;
  spec: PriceSpec;
  star?: boolean;
  suffix?: string;
}) {
  const hit = !!matchTier(pricing, spec);
  return (
    <td
      className={cn(
        "px-2.5 py-2 text-right tabular-nums",
        hit && "text-sky-600 dark:text-sky-400",
        star && "font-semibold",
      )}
    >
      {quote(pricing, caps, spec)}
      {suffix}
      {star && <Star className="ml-1 inline size-3 text-sky-500" aria-label="默认参数" />}
    </td>
  );
}

function Legend({ unit }: { unit: string }) {
  return (
    <div className="text-muted-foreground mt-2 flex flex-wrap gap-3 text-[11px]">
      <span className="inline-flex items-center gap-1">
        <Star className="size-3 text-sky-500" />
        默认参数
      </span>
      <span className="text-sky-600 dark:text-sky-400">蓝色：命中规格价格</span>
      <span>{unit}，均为生成 1 个的价格</span>
    </div>
  );
}

/**
 * 积分定价页签的预览：用户点「生成」前看到的价格，与画布用同一份计价算法。
 * 按次：一个规格维度是「档位 → 积分」表，两个是矩阵；按秒：「档位 × 每秒价 / 最短 / 默认 / 最长时长的总价」表；
 * Token：输入 / 输出价与一个示例估算。
 */
export function PricePreview({
  pricing,
  caps,
}: {
  pricing: Pricing | null;
  caps: Capabilities | undefined;
}) {
  const note =
    "价格在用户点「生成」前就会显示在按钮上；生成多个时按钮上是合计。积分成本只有管理员能看到。";
  if (!pricing) {
    return (
      <PreviewFrame title="用户看到的价格" note={note}>
        <p className="text-muted-foreground text-xs">还没有配置定价。</p>
      </PreviewFrame>
    );
  }
  const base = defaultSpec(caps);
  const dims = previewDims(caps);
  const [row, col] = dims;
  const at = (overrides: Record<string, unknown>): PriceSpec => ({
    ...base,
    params: { ...base.params, ...overrides },
  });
  const isDefault = (dim: PreviewDim | undefined, value: unknown) =>
    !dim || String(base.params[dim.name]) === String(value);

  if (pricing.billing === "token") {
    const output = caps?.context?.output ?? 0;
    const sample = quote(pricing, caps, { ...base, promptChars: 1000 });
    return (
      <PreviewFrame title="用户看到的价格" note={note}>
        <div className="bg-card space-y-2 rounded-xl border p-4 text-sm">
          <div className="flex justify-between">
            <span className="text-muted-foreground">输入</span>
            <span className="tabular-nums">{pricing.token?.in ?? 0} 积分 / 百万 Token</span>
          </div>
          <div className="flex justify-between">
            <span className="text-muted-foreground">输出</span>
            <span className="tabular-nums">{pricing.token?.out ?? 0} 积分 / 百万 Token</span>
          </div>
          <div className="text-muted-foreground border-t pt-2 text-xs leading-relaxed">
            例：1000 字提示词、最大输出 {output} Token，提交时最多冻结{" "}
            <b className="text-foreground">{sample}</b> 积分，完成后按实际用量结算。
          </div>
        </div>
      </PreviewFrame>
    );
  }

  if (pricing.billing === "per_second") {
    const duration = caps?.params?.duration;
    const points = [
      ["最短", duration?.min],
      ["默认", duration?.default],
      ["最长", duration?.max],
    ].filter((item): item is [string, number] => typeof item[1] === "number");
    const rows = row ? row.values : [{ value: undefined, text: "全部" }];
    return (
      <PreviewFrame title="用户看到的价格" note={note}>
        <div className="bg-card overflow-hidden rounded-xl border">
          <table className="w-full text-xs">
            <thead>
              <tr className="bg-muted/40 border-b">
                <th className="text-muted-foreground px-2.5 py-2 text-left font-medium">
                  {row?.label ?? "规格"}
                </th>
                <th className="px-2.5 py-2 text-right font-medium">每秒</th>
                {points.map(([text, seconds]) => (
                  <th key={text} className="px-2.5 py-2 text-right font-medium">
                    {text} {seconds}秒
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => {
                const over = row ? { [row.name]: item.value } : {};
                return (
                  <tr key={String(item.value)} className="border-b last:border-0">
                    <td className="px-2.5 py-2 font-medium">{item.text}</td>
                    <Cell
                      pricing={pricing}
                      caps={caps}
                      spec={at({ ...over, duration: 1 })}
                      star={isDefault(row, item.value)}
                    />
                    {points.map(([text, seconds]) => (
                      <Cell
                        key={text}
                        pricing={pricing}
                        caps={caps}
                        spec={at({ ...over, duration: seconds })}
                      />
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <Legend unit="单位：积分（每秒一列是积分 / 秒）" />
      </PreviewFrame>
    );
  }

  if (!row) {
    return (
      <PreviewFrame title="用户看到的价格" note={note}>
        <div className="bg-card rounded-xl border p-4">
          <div className="text-muted-foreground text-xs">默认参数下每次消耗</div>
          <div className="mt-1 text-3xl font-bold tabular-nums">
            {quote(pricing, caps, base)}
            <span className="text-muted-foreground ml-1 text-sm font-normal">积分</span>
          </div>
        </div>
      </PreviewFrame>
    );
  }

  return (
    <PreviewFrame title="用户看到的价格" note={note}>
      <div className="bg-card overflow-hidden rounded-xl border">
        <table className="w-full text-xs">
          <thead>
            <tr className="bg-muted/40 border-b">
              <th className="text-muted-foreground px-2.5 py-2 text-left font-medium">
                {row.label}
                {col && ` \\ ${col.label}`}
              </th>
              {(col ? col.values : [{ value: undefined, text: "积分" }]).map((item) => (
                <th key={String(item.value)} className="px-2.5 py-2 text-right font-medium">
                  {item.text}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {row.values.map((r) => (
              <tr key={String(r.value)} className="border-b last:border-0">
                <td className="px-2.5 py-2 font-medium">{r.text}</td>
                {(col ? col.values : [{ value: undefined, text: "" }]).map((c) => (
                  <Cell
                    key={String(c.value)}
                    pricing={pricing}
                    caps={caps}
                    spec={at({ [row.name]: r.value, ...(col ? { [col.name]: c.value } : {}) })}
                    star={isDefault(row, r.value) && isDefault(col, c.value)}
                  />
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Legend unit="单位：积分 / 次" />
    </PreviewFrame>
  );
}
