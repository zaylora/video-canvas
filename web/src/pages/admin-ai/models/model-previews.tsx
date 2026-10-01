import type { ReactNode } from "react";
import { Eye, Star } from "lucide-react";

import type { ConfigListItem } from "@/api/admin-ai/type";
import type { InputSchema } from "@/api/model/type";
import { InitialAvatar } from "@/components/admin-ui/initial-avatar";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { schemaFields } from "@/utils/tasks/input-schema";

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
  credits,
  hint,
  active,
}: {
  name: string;
  seed: string;
  credits?: number | null;
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
      <InitialAvatar name={name} seed={seed} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm font-medium">{name || "未命名模型"}</span>
          {credits !== undefined && credits !== null && (
            <span className="text-muted-foreground ml-auto shrink-0 text-xs tabular-nums">
              {credits} 积分
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
  credits,
  hint,
  models,
}: {
  modelKey: string;
  label: string;
  kind: string;
  credits: number | null;
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
        <PickerRow active name={label} seed={modelKey} credits={credits} hint={hint} />
        {others.map((item) => (
          <PickerRow key={item.key} name={item.label || item.name || item.key} seed={item.key} />
        ))}
      </div>
    </PreviewFrame>
  );
}

/** 可以拿来区分价格的规格参数：有选项的 enum 字段（时长、分辨率等） */
export const specFields = (schema: InputSchema | undefined) =>
  schemaFields(schema).filter((field) => field.type === "enum" && !!field.options?.length);

/**
 * 积分定价页签的预览：用户点“生成”前看到的价格。
 * 有两个以上规格参数时画成“规格 × 规格”价格矩阵（星标是默认参数）；规格价格还没支持，所以格子都是统一价。
 */
export function PricePreview({
  credits,
  schema,
}: {
  credits: number | null;
  schema: InputSchema | undefined;
}) {
  const price = credits ?? 0;
  const [row, col] = specFields(schema);
  return (
    <PreviewFrame
      title="用户看到的价格"
      note="价格在用户点“生成”前就会显示在按钮上；积分成本只有管理员能看到。"
    >
      {row && col ? (
        <>
          <div className="bg-card overflow-hidden rounded-xl border">
            <table className="w-full text-xs">
              <thead>
                <tr className="bg-muted/40 border-b">
                  <th className="text-muted-foreground px-2.5 py-2 text-left font-medium">
                    {row.label} {"\\"} {col.label}
                  </th>
                  {col.options!.map((option) => (
                    <th key={String(option.value)} className="px-2.5 py-2 text-right font-medium">
                      {option.label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {row.options!.map((rowOption) => (
                  <tr key={String(rowOption.value)} className="border-b last:border-0">
                    <td className="px-2.5 py-2 font-medium">{rowOption.label}</td>
                    {col.options!.map((colOption) => {
                      const isDefault =
                        rowOption.value === row.default && colOption.value === col.default;
                      return (
                        <td
                          key={String(colOption.value)}
                          className={cn(
                            "px-2.5 py-2 text-right tabular-nums",
                            isDefault && "font-semibold",
                          )}
                        >
                          {price}
                          {isDefault && (
                            <Star
                              className="ml-1 inline size-3 text-sky-500"
                              aria-label="默认参数"
                            />
                          )}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="text-muted-foreground mt-2 flex flex-wrap gap-3 text-[11px]">
            <span className="inline-flex items-center gap-1">
              <Star className="size-3 text-sky-500" />
              默认参数
            </span>
            <span>单位：积分 / 次</span>
          </div>
        </>
      ) : (
        <div className="bg-card rounded-xl border p-4">
          <div className="text-muted-foreground text-xs">默认参数下每次消耗</div>
          <div className="mt-1 text-3xl font-bold tabular-nums">
            {price}
            <span className="text-muted-foreground ml-1 text-sm font-normal">积分</span>
          </div>
        </div>
      )}
    </PreviewFrame>
  );
}
