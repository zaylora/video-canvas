import { useState } from "react";
import { ArrowDown, ArrowUp, Plus, Star, Trash2, X } from "lucide-react";

import type {
  Capabilities,
  GenerationOp,
  ParamField,
  ParamOption,
  ParamSet,
  ParamType,
  RefKind,
  RefSpec,
} from "@/api/model/type";
import { Tag } from "@/components/admin-ui/tag";
import { ToggleChip } from "@/components/admin-ui/toggle-chip";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { OP_LABEL } from "@/utils/tasks/capabilities";

/** 一个字段的固定范围提示（来自后端固定上下限，和 backend/internal/provider/modelcfg/validate_caps.go 一致） */
export const LIMITS = {
  refMax: [1, 50],
  refMb: [1, 500],
  promptLength: [1, 1_000_000],
  numberValue: [1, 3600],
  enumOptions: 20,
  fanout: [1, 8],
  contextWindow: [1, 10_000_000],
  contextOutput: [256, 1_000_000],
} as const;

/** 各种类可选的生成方式（和后端 opsOfKind 一致） */
export const OPS_OF_KIND: Record<string, GenerationOp[]> = {
  video: ["t2v", "i2v", "omni"],
  image: ["t2i", "i2i"],
};

const OP_REFS: Record<GenerationOp, string> = {
  t2v: "只用文字",
  t2i: "只用文字",
  i2v: "图片",
  i2i: "图片",
  omni: "不限类型：已开启的参考素材都能引用，连什么上游节点就是什么",
};

const REF_LABEL: Record<RefKind, string> = {
  image: "参考图片",
  audio: "参考音频",
  video: "参考视频",
};
const REF_USED: Record<RefKind, string> = {
  image: "图生、全能参考",
  audio: "全能参考",
  video: "全能参考",
};

/** 整数输入框：空输入当作 0，超出范围标红（范围来自后端固定上下限） */
function IntInput({
  id,
  value,
  range,
  label,
  className,
  onChange,
}: {
  id?: string;
  value: number | undefined;
  range?: readonly [number, number];
  label: string;
  className?: string;
  onChange: (value: number | undefined) => void;
}) {
  const out = value !== undefined && range && (value < range[0] || value > range[1]);
  return (
    <Input
      id={id}
      type="number"
      inputMode="numeric"
      aria-label={label}
      aria-invalid={!!out}
      title={range ? `允许范围 ${range[0]} – ${range[1]}` : undefined}
      className={cn("h-8 w-24 tabular-nums", className)}
      value={value === undefined ? "" : String(value)}
      onChange={(event) => {
        const raw = event.target.value;
        onChange(raw === "" ? undefined : Math.trunc(Number(raw)));
      }}
    />
  );
}

/** 生成方式：勾选 chip（至少留一种），下方说明各方式可引用的素材 */
export function OpsEditor({
  kind,
  ops,
  onChange,
}: {
  kind: string;
  ops: GenerationOp[];
  onChange: (next: GenerationOp[]) => void;
}) {
  const all = OPS_OF_KIND[kind] ?? [];
  const toggle = (op: GenerationOp) => {
    const next = all.filter((item) => (item === op ? !ops.includes(op) : ops.includes(item)));
    if (next.length > 0) onChange(next);
  };
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2" role="group" aria-label="生成方式">
        {all.map((op) => (
          <ToggleChip key={op} pressed={ops.includes(op)} onClick={() => toggle(op)}>
            {OP_LABEL[op]}
          </ToggleChip>
        ))}
      </div>
      <table className="w-full text-xs">
        <tbody>
          {all.map((op) => (
            <tr
              key={op}
              className={cn("border-b last:border-0", !ops.includes(op) && "opacity-40")}
            >
              <td className="w-24 py-1.5 font-medium">{OP_LABEL[op]}</td>
              <td className="text-muted-foreground py-1.5">提示词 + {OP_REFS[op]}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** 文本模型的上下文能力 */
export function ContextEditor({
  value,
  onChange,
}: {
  value: { window: number; output: number } | undefined;
  onChange: (next: { window: number; output: number }) => void;
}) {
  const window = value?.window ?? 0;
  const output = value?.output ?? 0;
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-3 text-sm">
      <label className="flex items-center gap-2">
        上下文窗口
        <IntInput
          id="model-context-window"
          label="上下文窗口"
          value={window}
          range={LIMITS.contextWindow}
          className="w-32"
          onChange={(next) => onChange({ window: next ?? 0, output })}
        />
        <span className="text-muted-foreground text-xs">Token</span>
      </label>
      <label className="flex items-center gap-2">
        最大输出
        <IntInput
          label="最大输出"
          value={output}
          range={LIMITS.contextOutput}
          className="w-28"
          onChange={(next) => onChange({ window, output: next ?? 0 })}
        />
        <span className="text-muted-foreground text-xs">Token，须小于窗口</span>
      </label>
    </div>
  );
}

/** 一种参考素材：开关、最多数量、单个上限 MB、数量进度条 */
export function RefCardEditor({
  kind,
  value,
  onChange,
}: {
  kind: RefKind;
  value: RefSpec;
  onChange: (next: RefSpec) => void;
}) {
  return (
    <div className={cn("rounded-lg border p-3", !value.on && "bg-muted/30")}>
      <div className="flex items-center gap-2">
        <Switch
          size="sm"
          checked={value.on}
          aria-label={`接收${REF_LABEL[kind]}`}
          onCheckedChange={(on) =>
            onChange(
              on
                ? { on, max: value.max || 1, max_mb: value.max_mb || 10 }
                : { on, max: value.max, max_mb: value.max_mb },
            )
          }
        />
        <span className="text-sm font-medium">{REF_LABEL[kind]}</span>
        <span className="text-muted-foreground ml-auto text-xs">用于：{REF_USED[kind]}</span>
      </div>
      {value.on && (
        <div className="mt-3 space-y-2">
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-xs">
            <label className="flex items-center gap-2">
              最多
              <IntInput
                label={`${REF_LABEL[kind]}最多数量`}
                value={value.max}
                range={LIMITS.refMax}
                className="w-20"
                onChange={(max) => onChange({ ...value, max: max ?? 0 })}
              />
              个
            </label>
            <label className="flex items-center gap-2">
              单个不超过
              <IntInput
                label={`${REF_LABEL[kind]}单个大小上限`}
                value={value.max_mb}
                range={LIMITS.refMb}
                className="w-20"
                onChange={(max_mb) => onChange({ ...value, max_mb: max_mb ?? 0 })}
              />
              MB
            </label>
          </div>
          <div className="bg-muted h-1.5 overflow-hidden rounded-full" aria-hidden>
            <div
              className="bg-foreground/70 h-full rounded-full"
              style={{ width: `${Math.min(100, (value.max / LIMITS.refMax[1]) * 100)}%` }}
            />
          </div>
        </div>
      )}
    </div>
  );
}

const PRESETS: Record<string, ParamOption[]> = {
  aspect_ratio: ["Auto", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"],
  resolution: ["480P", "720P", "1080P", "2K", "4K", "1K"],
  count: [1, 2, 4],
};
const presetsOf = (name: string): ParamOption[] =>
  PRESETS[name] ?? (name.includes("ratio") ? PRESETS.aspect_ratio : []);

/** enum 参数的可选值：勾选 / 输入添加，点星选默认值 */
function OptionsEditor({
  name,
  field,
  onChange,
}: {
  name: string;
  field: ParamField;
  onChange: (next: ParamField) => void;
}) {
  const [draft, setDraft] = useState("");
  const options = field.options ?? [];
  const setOptions = (next: ParamOption[]) => {
    const keepDefault = next.some((item) => String(item) === String(field.default));
    onChange({ ...field, options: next, default: keepDefault ? field.default : next[0] });
  };
  const add = (value: ParamOption) => {
    if (options.some((item) => String(item) === String(value))) return;
    if (options.length >= LIMITS.enumOptions) return;
    setOptions([...options, value]);
  };
  const commit = () => {
    const text = draft.trim();
    setDraft("");
    if (!text) return;
    const n = Number(text);
    add(Number.isFinite(n) && /^-?\d+(\.\d+)?$/.test(text) ? n : text);
  };
  const presets = presetsOf(name).filter(
    (preset) => !options.some((item) => String(item) === String(preset)),
  );
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1.5">
        {options.map((option) => {
          const isDefault = String(option) === String(field.default);
          return (
            <span
              key={String(option)}
              className={cn(
                "inline-flex h-7 items-center gap-1 rounded-md border pr-1 pl-1.5 text-xs",
                isDefault && "border-foreground/60 bg-accent",
              )}
            >
              <button
                type="button"
                aria-label={isDefault ? `${option} 是默认值` : `设 ${option} 为默认值`}
                aria-pressed={isDefault}
                onClick={() => onChange({ ...field, default: option })}
              >
                <Star
                  className={cn(
                    "size-3.5",
                    isDefault ? "fill-sky-500 text-sky-500" : "text-muted-foreground",
                  )}
                />
              </button>
              {String(option)}
              <button
                type="button"
                aria-label={`删除可选值 ${option}`}
                className="hover:bg-foreground/10 rounded-sm"
                onClick={() => setOptions(options.filter((item) => item !== option))}
              >
                <X className="size-3.5" />
              </button>
            </span>
          );
        })}
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <Input
          aria-label="添加可选值"
          placeholder="输入可选值，回车添加"
          className="h-7 w-44 text-xs"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing || event.key !== "Enter") return;
            event.preventDefault();
            commit();
          }}
          onBlur={commit}
        />
        {presets.map((preset) => (
          <button
            key={String(preset)}
            type="button"
            className="text-muted-foreground hover:text-foreground rounded-md border border-dashed px-1.5 py-0.5 text-xs"
            onClick={() => add(preset)}
          >
            + {String(preset)}
          </button>
        ))}
      </div>
    </div>
  );
}

/** number 参数：最小 / 最大 / 默认 / 步长，下方滑轨示意 */
function NumberEditor({
  field,
  onChange,
}: {
  field: ParamField;
  onChange: (next: ParamField) => void;
}) {
  const min = field.min;
  const max = field.max;
  const def = typeof field.default === "number" ? field.default : undefined;
  const step = field.step ?? 1;
  const set = (patch: Partial<ParamField>) => onChange({ ...field, ...patch });
  const pct =
    min !== undefined && max !== undefined && def !== undefined && max > min
      ? Math.min(100, Math.max(0, ((def - min) / (max - min)) * 100))
      : 0;
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
        {(
          [
            ["最小", "min", min],
            ["最大", "max", max],
            ["默认", "default", def],
            ["步长", "step", step],
          ] as const
        ).map(([text, key, value]) => (
          <label key={key} className="flex items-center gap-1.5">
            {text}
            <IntInput
              label={`${field.label}${text}`}
              value={value}
              range={key === "step" ? [1, LIMITS.numberValue[1]] : LIMITS.numberValue}
              className="w-16"
              onChange={(next) => set({ [key]: next } as Partial<ParamField>)}
            />
          </label>
        ))}
        <label className="flex items-center gap-1.5">
          单位
          <Input
            aria-label={`${field.label}单位`}
            className="h-8 w-14 text-xs"
            value={field.unit ?? ""}
            onChange={(event) => set({ unit: event.target.value || undefined })}
          />
        </label>
      </div>
      <div className="px-1">
        <div className="bg-muted relative h-1.5 rounded-full" aria-hidden>
          <div className="bg-foreground/40 h-full rounded-full" style={{ width: `${pct}%` }} />
          <span
            className="bg-foreground absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full"
            style={{ left: `${pct}%` }}
          />
        </div>
        <div className="text-muted-foreground mt-1 flex justify-between text-[10px] tabular-nums">
          <span>最短 {min ?? "?"}</span>
          <span>最长 {max ?? "?"}</span>
        </div>
      </div>
    </div>
  );
}

/** 一个生成参数：名称、类型、开放开关、取值设置、排序与删除 */
export function ParamRowEditor({
  name,
  field,
  first,
  last,
  error,
  onChange,
  onMove,
  onRemove,
}: {
  name: string;
  field: ParamField;
  first: boolean;
  last: boolean;
  error?: string;
  onChange: (next: ParamField) => void;
  onMove: (direction: -1 | 1) => void;
  onRemove: () => void;
}) {
  const canSpec = field.type === "enum" || field.type === "boolean";
  return (
    <div className={cn("rounded-lg border p-3", error && "border-destructive/50")}>
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label={`${name} 的展示名`}
          className="h-8 w-32 text-sm font-medium"
          value={field.label}
          onChange={(event) => onChange({ ...field, label: event.target.value })}
        />
        <code className="text-muted-foreground bg-muted rounded px-1.5 py-0.5 text-xs">{name}</code>
        <Tag>{{ enum: "枚举", number: "数字", boolean: "开关" }[field.type]}</Tag>
        {field.fanout && <Tag tone="violet">拆成 N 个任务</Tag>}
        {field.spec && <Tag tone="info">规格价格维度</Tag>}
        <label className="ml-auto flex items-center gap-1.5 text-xs">
          开放给用户
          <Switch
            size="sm"
            checked={field.open}
            aria-label={`${field.label || name} 开放给用户`}
            onCheckedChange={(open) => onChange({ ...field, open })}
          />
        </label>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="上移"
          disabled={first}
          onClick={() => onMove(-1)}
        >
          <ArrowUp />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="下移"
          disabled={last}
          onClick={() => onMove(1)}
        >
          <ArrowDown />
        </Button>
        <Button variant="ghost" size="icon-xs" aria-label={`删除参数 ${name}`} onClick={onRemove}>
          <Trash2 />
        </Button>
      </div>
      <div className="mt-3">
        {field.type === "enum" && <OptionsEditor name={name} field={field} onChange={onChange} />}
        {field.type === "number" && <NumberEditor field={field} onChange={onChange} />}
        {field.type === "boolean" && (
          <label className="flex items-center gap-2 text-xs">
            默认开启
            <Switch
              size="sm"
              checked={field.default === true}
              aria-label={`${field.label || name} 默认开启`}
              onCheckedChange={(on) => onChange({ ...field, default: on })}
            />
          </label>
        )}
      </div>
      {(canSpec || !field.open) && (
        <div className="text-muted-foreground mt-3 flex flex-wrap items-center gap-x-4 text-xs">
          {canSpec && (
            <label className="flex items-center gap-1.5">
              作为规格价格维度
              <Switch
                size="sm"
                checked={!!field.spec}
                aria-label={`${field.label || name} 作为规格价格维度`}
                onCheckedChange={(spec) => onChange({ ...field, spec: spec || undefined })}
              />
            </label>
          )}
          {!field.open && <span>不开放：画布上不出现，提交时按默认值发送</span>}
        </div>
      )}
      {error && <p className="text-destructive mt-2 text-xs">{error}</p>}
    </div>
  );
}

const NEW_NAME_RE = /^[a-z][a-z0-9_]{0,31}$/;

/** 新增一个生成参数：参数名（英文，要和插件约定的键对应）+ 展示名 + 类型 */
export function AddParamRow({
  existing,
  onAdd,
}: {
  existing: string[];
  onAdd: (name: string, field: ParamField) => void;
}) {
  const [name, setName] = useState("");
  const [label, setLabel] = useState("");
  const [type, setType] = useState<ParamType>("enum");
  const nameOk = NEW_NAME_RE.test(name) && !existing.includes(name);
  const add = () => {
    if (!nameOk) return;
    const base = { label: label.trim() || name, open: true };
    const field: ParamField =
      type === "enum"
        ? { type, ...base, options: [], default: undefined }
        : type === "number"
          ? { type, ...base, min: 1, max: 10, step: 1, default: 1 }
          : { type, ...base, default: false };
    onAdd(name, field);
    setName("");
    setLabel("");
  };
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-dashed p-2.5">
      <Input
        aria-label="新参数名"
        placeholder="参数名，如 seed"
        className="h-8 w-36 font-mono text-xs"
        aria-invalid={!!name && !nameOk}
        title="小写字母、数字、下划线，字母开头；它会作为任务输入的键传给插件"
        value={name}
        onChange={(event) => setName(event.target.value)}
      />
      <Input
        aria-label="新参数展示名"
        placeholder="展示名"
        className="h-8 w-32 text-xs"
        value={label}
        onChange={(event) => setLabel(event.target.value)}
      />
      <select
        aria-label="新参数类型"
        className="border-input bg-background h-8 rounded-lg border px-2 text-xs"
        value={type}
        onChange={(event) => setType(event.target.value as ParamType)}
      >
        <option value="enum">枚举</option>
        <option value="number">数字</option>
        <option value="boolean">开关</option>
      </select>
      <Button size="sm" variant="outline" disabled={!nameOk} onClick={add}>
        <Plus />
        添加参数
      </Button>
    </div>
  );
}

/** 在有序参数集里改 / 移 / 删 / 加一项，返回新的 ParamSet（保持书写顺序） */
export const paramSetOps = {
  patch(set: ParamSet, name: string, field: ParamField): ParamSet {
    return Object.fromEntries(Object.entries(set).map(([k, v]) => [k, k === name ? field : v]));
  },
  move(set: ParamSet, name: string, direction: -1 | 1): ParamSet {
    const entries = Object.entries(set);
    const index = entries.findIndex(([k]) => k === name);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= entries.length) return set;
    [entries[index], entries[target]] = [entries[target], entries[index]];
    return Object.fromEntries(entries);
  },
  remove(set: ParamSet, name: string): ParamSet {
    return Object.fromEntries(Object.entries(set).filter(([k]) => k !== name));
  },
  add(set: ParamSet, name: string, field: ParamField): ParamSet {
    return { ...set, [name]: field };
  },
};

export type { Capabilities };
