import type { ReactNode } from "react";
import { AudioLines, Film, Image as ImageIcon, Link2, Star, type LucideIcon } from "lucide-react";

import type { InputFieldSchema, InputSchema } from "@/api/model/type";
import { Tag } from "@/components/admin-ui/tag";
import { ToggleChip } from "@/components/admin-ui/toggle-chip";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { schemaFields, type SchemaField } from "@/utils/tasks/input-schema";

const TYPE_LABEL: Record<string, string> = {
  text: "文本",
  number: "数字",
  enum: "选项",
  boolean: "开关",
  image: "图片",
  video: "视频",
  audio: "音频",
};

const MEDIA_ICON: Partial<Record<string, LucideIcon>> = {
  image: ImageIcon,
  video: Film,
  audio: AudioLines,
};

export const isMediaField = (field: InputFieldSchema) =>
  field.type === "image" || field.type === "video" || field.type === "audio";

/** 改 schema 里一个字段的一个属性，字段顺序与其余属性不动；value 为 undefined 时删掉该属性 */
function patchField(
  schema: InputSchema,
  name: string,
  key: keyof InputFieldSchema,
  value: unknown,
): InputSchema {
  return Object.fromEntries(
    Object.entries(schema).map(([fieldName, field]) => {
      if (fieldName !== name) return [fieldName, field];
      const next: Record<string, unknown> = { ...field };
      if (value === undefined) delete next[key];
      else next[key] = value;
      return [fieldName, next];
    }),
  ) as InputSchema;
}

const numberOrUndefined = (text: string) => {
  if (text.trim() === "") return undefined;
  const value = Number(text);
  return Number.isFinite(value) ? value : undefined;
};

function SmallLabel({ children }: { children: ReactNode }) {
  return <div className="text-muted-foreground mb-1.5 text-[11px]">{children}</div>;
}

/** 一个字段的配置行（设计稿 optRow）：标题 + 必填 / 高级开关，下面按类型给出可改的属性 */
function FieldRow({
  field,
  onPatch,
}: {
  field: SchemaField;
  onPatch: (key: keyof InputFieldSchema, value: unknown) => void;
}) {
  const options = field.options ?? [];
  const MediaIcon = MEDIA_ICON[field.type];
  return (
    <div className="space-y-3 p-4">
      <div className="flex flex-wrap items-start gap-3">
        {MediaIcon && (
          <span className="bg-muted grid size-8 shrink-0 place-items-center rounded-md">
            <MediaIcon className="size-4" />
          </span>
        )}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
            {field.label || field.name}
            <span className="text-muted-foreground font-mono text-xs font-normal">
              {field.name} · {TYPE_LABEL[field.type] ?? field.type}
            </span>
            {field.port && (
              <Tag title="可以由上游节点连线提供">
                <Link2 />
                可连线
              </Tag>
            )}
          </div>
        </div>
        <label className="text-muted-foreground flex shrink-0 items-center gap-2 text-xs">
          必填
          <Switch
            size="sm"
            checked={!!field.required}
            onCheckedChange={(checked) => onPatch("required", checked || undefined)}
          />
        </label>
        <label className="text-muted-foreground flex shrink-0 items-center gap-2 text-xs">
          收进「高级」
          <Switch
            size="sm"
            checked={!!field.advanced}
            onCheckedChange={(checked) => onPatch("advanced", checked || undefined)}
          />
        </label>
      </div>

      {field.type === "enum" && (
        <>
          <div>
            <SmallLabel>可选值</SmallLabel>
            <div className="flex flex-wrap gap-2">
              {options.map((option) => (
                <ToggleChip key={String(option.value)} pressed className="pointer-events-none">
                  {option.label ?? String(option.value)}
                </ToggleChip>
              ))}
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-muted-foreground text-[11px]">默认值</span>
            {options.map((option) => {
              const on = String(field.default) === String(option.value);
              return (
                <button
                  key={String(option.value)}
                  type="button"
                  aria-pressed={on}
                  onClick={() => onPatch("default", on ? undefined : option.value)}
                  className={cn(
                    "inline-flex h-6 items-center gap-1 rounded-full border px-2.5 text-[11px]",
                    on
                      ? "border-sky-500/50 bg-sky-500/15 text-sky-600 dark:text-sky-400"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {on && <Star className="size-3" />}
                  {option.label ?? String(option.value)}
                </button>
              );
            })}
          </div>
        </>
      )}

      {field.type === "boolean" && (
        <label className="flex items-center gap-2 text-xs">
          <span className="text-muted-foreground">默认</span>
          <Switch
            size="sm"
            checked={field.default === true}
            onCheckedChange={(checked) => onPatch("default", checked)}
          />
          {field.default === true ? "开" : "关"}
        </label>
      )}

      {field.type === "number" && (
        <div className="grid grid-cols-3 gap-3">
          {(["min", "max", "default"] as const).map((key) => (
            <div key={key}>
              <SmallLabel>{{ min: "最小", max: "最大", default: "默认" }[key]}</SmallLabel>
              <Input
                type="number"
                className="tabular-nums"
                aria-label={`${field.label} ${key}`}
                value={typeof field[key] === "number" ? String(field[key]) : ""}
                onChange={(event) => onPatch(key, numberOrUndefined(event.target.value))}
              />
            </div>
          ))}
        </div>
      )}

      {field.type === "text" && (
        <div className="sm:w-1/2">
          <SmallLabel>最大字数</SmallLabel>
          <Input
            type="number"
            min={1}
            className="tabular-nums"
            placeholder="不限"
            aria-label={`${field.label} 最大字数`}
            value={field.max_length ? String(field.max_length) : ""}
            onChange={(event) => onPatch("max_length", numberOrUndefined(event.target.value))}
          />
        </div>
      )}

      {MediaIcon && (
        <p className="text-muted-foreground text-xs">
          用户可以上传，或从上游{TYPE_LABEL[field.type]}节点连线提供。
        </p>
      )}
    </div>
  );
}

/**
 * input_schema 的图形化配置：按书写顺序逐个字段列出，改的是默认值、必填、收进高级、数字范围、字数上限。
 * 增删字段、改选项等结构性修改仍用下方的 JSON。
 * @param media 只列素材字段（参考素材）还是只列其余字段（生成参数）
 */
export function SchemaFieldEditor({
  schema,
  media,
  onChange,
}: {
  schema: InputSchema;
  media: boolean;
  onChange: (schema: InputSchema) => void;
}) {
  const fields = schemaFields(schema).filter((field) => isMediaField(field) === media);
  if (fields.length === 0)
    return (
      <p className="text-muted-foreground rounded-lg border border-dashed p-4 text-center text-xs">
        {media ? "这个模型不接收参考素材。" : "还没有生成参数。"}在下方 JSON 里添加字段。
      </p>
    );
  return (
    <div className="divide-y rounded-lg border">
      {fields.map((field) => (
        <FieldRow
          key={field.name}
          field={field}
          onPatch={(key, value) => onChange(patchField(schema, field.name, key, value))}
        />
      ))}
    </div>
  );
}
