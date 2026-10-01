import { useRef, useState } from "react";
import { Link2, Loader2, Upload, X } from "lucide-react";

import { uploadAsset } from "@/api/asset";
import type { Capabilities, GenerationOp, RefKind } from "@/api/model/type";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import type { ParamAsset } from "@/types";
import {
  effectiveValue,
  manualRefs,
  openParams,
  refKindsOf,
  REF_KEYS,
  toAssetNumber,
  type Bindings,
  type ParamEntry,
  type RefKey,
} from "@/utils/tasks/capabilities";

/** 画布里已经有的、可以直接拿来当参考素材的素材 */
export type AssetChoice = {
  assetId: string;
  url: string;
  label: string;
  mediaType: RefKind;
};

type VideoParamPanelProps = {
  caps: Capabilities;
  op?: GenerationOp;
  params: Record<string, unknown>;
  paramAssets?: Record<string, ParamAsset>;
  /** 每个输入口由哪些上游连线提供 */
  bindings: Bindings;
  /** 输入名 -> 标红原因 */
  errors: Record<string, string>;
  /** 点过生成之后才标红，避免刚打开面板就一片红 */
  showErrors: boolean;
  disabled?: boolean;
  onChange: (name: string, value: unknown) => void;
  onAddRef: (key: RefKey, assetId: string | number, asset: ParamAsset) => void;
  onRemoveRef: (key: RefKey, assetId: string | number) => void;
  listAssets: (type: RefKind) => AssetChoice[];
};

const ACCEPT: Record<RefKind, string> = { image: "image/*", video: "video/*", audio: "audio/*" };

const RATIO_RE = /^(\d+):(\d+)$/;
const isRatioParam = (field: ParamEntry) =>
  field.type === "enum" &&
  (field.options ?? []).every(
    (option) => RATIO_RE.test(String(option)) || /^auto$/i.test(String(option)),
  );

/** 比例按钮上的小图形：按宽高比画一个圆角矩形，Auto 画虚线框 */
function RatioShape({ option }: { option: string }) {
  const match = RATIO_RE.exec(option);
  const [w, h] = match ? [Number(match[1]), Number(match[2])] : [1, 1];
  const scale = 16 / Math.max(w, h);
  return (
    <span
      aria-hidden
      className={cn("border-current border-[1.5px]", !match && "border-dashed")}
      style={{ width: w * scale, height: h * scale, borderRadius: 3 }}
    />
  );
}

/** 比例：带比例图形的网格按钮 */
function RatioGrid({
  field,
  value,
  disabled,
  onChange,
}: {
  field: ParamEntry;
  value: unknown;
  disabled?: boolean;
  onChange: (value: unknown) => void;
}) {
  return (
    <div role="radiogroup" aria-label={field.label} className="grid grid-cols-4 gap-1">
      {(field.options ?? []).map((option) => {
        const active = String(option) === String(value);
        return (
          <button
            key={String(option)}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            className={cn(
              "flex flex-col items-center gap-1 rounded-lg border px-1 py-1.5 text-[11px] transition-colors disabled:opacity-50",
              active
                ? "border-foreground/60 bg-accent text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
            onClick={() => onChange(option)}
          >
            <RatioShape option={String(option)} />
            {String(option)}
          </button>
        );
      })}
    </div>
  );
}

/** 等宽分段按钮，一眼看全；选项很多时退回下拉 */
function SegmentedControl({
  label,
  options,
  value,
  disabled,
  invalid,
  onChange,
}: {
  label: string;
  options: Array<{ value: string | number | boolean; text: string }>;
  value: unknown;
  disabled?: boolean;
  invalid?: boolean;
  onChange: (value: unknown) => void;
}) {
  if (options.length > 6) {
    return (
      <select
        aria-label={label}
        aria-invalid={invalid}
        disabled={disabled}
        value={value === undefined ? "" : String(value)}
        className="border-input bg-transparent focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:border-destructive h-8 w-full rounded-lg border px-2 text-xs outline-none focus-visible:ring-3 disabled:opacity-50 dark:bg-input/30"
        onChange={(event) => {
          const option = options.find((item) => String(item.value) === event.target.value);
          onChange(option ? option.value : undefined);
        }}
      >
        {value === undefined && <option value="">请选择</option>}
        {options.map((option) => (
          <option key={String(option.value)} value={String(option.value)}>
            {option.text}
          </option>
        ))}
      </select>
    );
  }
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className={cn(
        "bg-muted flex gap-0.5 rounded-lg p-0.5",
        invalid && "ring-destructive/40 ring-1",
      )}
    >
      {options.map((option) => {
        const active = String(option.value) === String(value);
        return (
          <button
            key={String(option.value)}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            className={cn(
              "min-w-0 flex-1 truncate rounded-md px-2 py-1 text-xs transition-colors disabled:opacity-50",
              active
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground",
            )}
            onClick={() => onChange(option.value)}
          >
            {option.text}
          </button>
        );
      })}
    </div>
  );
}

/** 数字参数（如时长）：滑块 + 数字输入框 + 单位，滑块下标注最短 / 最长 */
function NumberControl({
  field,
  value,
  disabled,
  invalid,
  onChange,
}: {
  field: ParamEntry;
  value: unknown;
  disabled?: boolean;
  invalid?: boolean;
  onChange: (value: unknown) => void;
}) {
  const min = field.min ?? 0;
  const max = field.max ?? min;
  const n = typeof value === "number" ? value : Number(value);
  const current = Number.isFinite(n) ? n : min;
  const unit = field.unit ?? "";
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <input
          type="range"
          aria-label={field.label}
          min={min}
          max={max}
          step={field.step ?? 1}
          value={Math.min(Math.max(current, min), max)}
          disabled={disabled}
          className="nodrag accent-foreground h-1.5 min-w-0 flex-1 cursor-pointer disabled:opacity-50"
          onChange={(event) => onChange(Number(event.target.value))}
        />
        <Input
          type="number"
          inputMode="numeric"
          min={min}
          max={max}
          step={field.step ?? 1}
          disabled={disabled}
          aria-invalid={invalid}
          aria-label={`${field.label}（数值）`}
          className="h-7 w-16 px-2 text-xs tabular-nums"
          value={value === undefined || value === null ? "" : String(value)}
          onChange={(event) => {
            const raw = event.target.value;
            onChange(raw === "" ? undefined : Number(raw));
          }}
        />
        {unit && <span className="text-muted-foreground text-xs">{unit}</span>}
      </div>
      <div className="text-muted-foreground flex justify-between text-[10px]">
        <span>
          最短 {min}
          {unit}
        </span>
        <span>
          最长 {max}
          {unit}
        </span>
      </div>
    </div>
  );
}

function ParamRow({
  field,
  params,
  error,
  disabled,
  onChange,
}: {
  field: ParamEntry;
  params: Record<string, unknown>;
  error?: string;
  disabled?: boolean;
  onChange: (name: string, value: unknown) => void;
}) {
  const value = effectiveValue(field, params, field.name);
  const invalid = !!error;
  const set = (next: unknown) => onChange(field.name, next);
  let control;
  if (field.type === "number") {
    control = (
      <NumberControl
        field={field}
        value={value}
        disabled={disabled}
        invalid={invalid}
        onChange={set}
      />
    );
  } else if (field.type === "boolean") {
    control = (
      <SegmentedControl
        label={field.label}
        options={[
          { value: true, text: "开启" },
          { value: false, text: "关闭" },
        ]}
        value={value === true}
        disabled={disabled}
        onChange={set}
      />
    );
  } else if (isRatioParam(field)) {
    control = <RatioGrid field={field} value={value} disabled={disabled} onChange={set} />;
  } else {
    control = (
      <SegmentedControl
        label={field.label}
        options={(field.options ?? []).map((option) => ({
          value: option,
          text: `${option}${field.unit ?? ""}`,
        }))}
        value={value}
        disabled={disabled}
        invalid={invalid}
        onChange={set}
      />
    );
  }
  return (
    <div className="flex flex-col gap-1">
      <span className="text-muted-foreground text-xs">{field.label}</span>
      {control}
      {error && <p className="text-destructive text-xs">{error}</p>}
    </div>
  );
}

/** 一种参考素材：已连接 / 上限、来源、手动添加的素材，以及上传 / 选画布素材 */
function RefCard({
  refKey,
  kind,
  label,
  max,
  maxMb,
  links,
  manual,
  assets,
  error,
  disabled,
  onAdd,
  onRemove,
  listAssets,
}: {
  refKey: RefKey;
  kind: RefKind;
  label: string;
  max: number;
  maxMb: number;
  links: Bindings["images"];
  manual: string[];
  assets?: Record<string, ParamAsset>;
  error?: string;
  disabled?: boolean;
  onAdd: VideoParamPanelProps["onAddRef"];
  onRemove: VideoParamPanelProps["onRemoveRef"];
  listAssets: VideoParamPanelProps["listAssets"];
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [choices, setChoices] = useState<AssetChoice[]>([]);
  const linkedIds = links.map((link) => toAssetNumber(link.assetId)).filter((id) => id !== null);
  const used = new Set([...linkedIds.map(String), ...manual]).size;
  const full = used >= max;

  const upload = async (file: File) => {
    if (file.size > maxMb * 1024 * 1024) {
      setUploadError(`单个${label}不能超过 ${maxMb} MB`);
      return;
    }
    setUploading(true);
    setUploadError(null);
    try {
      const uploaded = await uploadAsset(file);
      onAdd(refKey, uploaded.id, {
        url: uploaded.url,
        fileName: uploaded.fileName ?? file.name,
        mediaType: kind,
      });
    } catch {
      setUploadError("上传失败，请重试");
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className={cn("tabular-nums", full ? "text-foreground" : "text-muted-foreground")}>
          {used} / {max}
        </span>
      </div>
      {links.map((link) => (
        <span
          key={link.edgeId}
          className="bg-muted text-muted-foreground inline-flex max-w-full items-center gap-1 self-start rounded-md px-1.5 py-0.5 text-xs"
          title={`来自：${link.sourceLabel}`}
        >
          <Link2 className="size-3 shrink-0" />
          <span className="truncate">来自：{link.sourceLabel}</span>
        </span>
      ))}
      {manual.map((assetId) => {
        const asset = assets?.[assetId];
        return (
          <div
            key={assetId}
            className="bg-muted/50 flex items-center gap-2 rounded-lg border p-1.5"
          >
            {asset?.url && kind === "image" && (
              <img src={asset.url} alt="" className="size-9 rounded object-cover" />
            )}
            {asset?.url && kind === "video" && (
              <video src={asset.url} muted className="size-9 rounded object-cover" />
            )}
            <span className="min-w-0 flex-1 truncate text-xs">
              {asset?.fileName ?? `素材 #${assetId}`}
            </span>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="移除素材"
              disabled={disabled}
              onClick={() => onRemove(refKey, assetId)}
            >
              <X />
            </Button>
          </div>
        );
      })}
      <div className="flex gap-1.5">
        <Button
          variant="outline"
          size="xs"
          disabled={disabled || uploading || full}
          onClick={() => inputRef.current?.click()}
        >
          {uploading ? <Loader2 className="animate-spin" /> : <Upload />}
          上传
        </Button>
        <DropdownMenu
          modal={false}
          onOpenChange={(open) => {
            if (open) setChoices(listAssets(kind));
          }}
        >
          <DropdownMenuTrigger
            disabled={disabled || full}
            className="border-border bg-background hover:bg-muted inline-flex h-7 items-center gap-1.5 rounded-[min(var(--radius-md),10px)] border px-2 text-xs font-medium disabled:opacity-50"
          >
            <Link2 className="size-3.5" />
            画布素材
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-56" align="start" sideOffset={6}>
            <DropdownMenuGroup>
              <DropdownMenuLabel>选择画布里已有的素材</DropdownMenuLabel>
              {choices.length === 0 && (
                <p className="text-muted-foreground px-1.5 py-2 text-xs">
                  画布里还没有可用的{label}
                </p>
              )}
              {choices.map((choice) => (
                <DropdownMenuItem
                  key={choice.assetId}
                  onClick={() =>
                    onAdd(refKey, choice.assetId, {
                      url: choice.url,
                      fileName: choice.label,
                      mediaType: choice.mediaType,
                    })
                  }
                >
                  <span className="truncate">{choice.label}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <span className="text-muted-foreground self-center text-[10px]">单个 ≤ {maxMb} MB</span>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept={ACCEPT[kind]}
        className="sr-only"
        aria-hidden
        tabIndex={-1}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) void upload(file);
        }}
      />
      {(error || uploadError) && <p className="text-destructive text-xs">{uploadError ?? error}</p>}
    </div>
  );
}

/**
 * 按模型能力（capabilities）渲染的参数面板：先是当前生成方式能引用的参考素材（每种一张卡，
 * 显示已连接 / 上限），再是开放给用户的生成参数（按 capabilities.params 的书写顺序）。
 * 提示词由输入框负责，生成方式由底栏的下拉负责，这里不重复画。
 */
export function VideoParamPanel({
  caps,
  op,
  params,
  paramAssets,
  bindings,
  errors,
  showErrors,
  disabled,
  onChange,
  onAddRef,
  onRemoveRef,
  listAssets,
}: VideoParamPanelProps) {
  const kinds = refKindsOf(caps, op);
  const fields = openParams(caps);
  const textOnly = !!op && kinds.length === 0;
  if (kinds.length === 0 && fields.length === 0 && !textOnly) return null;

  return (
    <div className="nowheel flex max-h-72 flex-col gap-2.5 overflow-y-auto px-1 py-0.5">
      {textOnly && (
        <p className="text-muted-foreground text-xs">当前方式只用文字，不使用参考素材</p>
      )}
      {REF_KEYS.filter((ref) => kinds.includes(ref.kind)).map((ref) => (
        <RefCard
          key={ref.key}
          refKey={ref.key}
          kind={ref.kind}
          label={ref.label}
          max={caps.refs[ref.kind].max}
          maxMb={caps.refs[ref.kind].max_mb}
          links={bindings[ref.key]}
          manual={manualRefs(params, ref.key).map(String)}
          assets={paramAssets}
          error={showErrors ? errors[ref.key] : undefined}
          disabled={disabled}
          onAdd={onAddRef}
          onRemove={onRemoveRef}
          listAssets={listAssets}
        />
      ))}
      {fields.map((field) => (
        <ParamRow
          key={field.name}
          field={field}
          params={params}
          error={showErrors ? errors[field.name] : undefined}
          disabled={disabled}
          onChange={onChange}
        />
      ))}
    </div>
  );
}
