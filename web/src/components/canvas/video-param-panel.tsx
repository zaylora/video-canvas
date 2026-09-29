import { useRef, useState } from "react";
import { ChevronDown, Link2, Loader2, Upload, X } from "lucide-react";

import { uploadAsset } from "@/api/asset";
import type { InputSchema } from "@/api/model/type";
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
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import type { ParamAsset } from "@/types";
import {
  effectiveValue,
  isMediaFieldType,
  schemaFields,
  type IncomingLink,
  type SchemaField,
} from "@/utils/tasks/input-schema";

/** 画布里已经有的、可以直接拿来当参数的素材 */
export type AssetChoice = {
  assetId: string;
  url: string;
  label: string;
  mediaType: "image" | "video" | "audio";
};

type VideoParamPanelProps = {
  schema: InputSchema;
  params: Record<string, unknown>;
  paramAssets?: Record<string, ParamAsset>;
  /** 字段名 -> 提供它的上游连线；有值的字段禁用手填 */
  bindings: Record<string, IncomingLink>;
  /** 字段名 -> 标红原因 */
  errors: Record<string, string>;
  /** 点过生成之后才标红，避免刚打开面板就一片红 */
  showErrors: boolean;
  disabled?: boolean;
  /** asset 为 null 表示清掉，undefined 表示不动展示信息 */
  onChange: (name: string, value: unknown, asset?: ParamAsset | null) => void;
  listAssets: (type: "image" | "video" | "audio") => AssetChoice[];
};

const ACCEPT: Record<"image" | "video" | "audio", string> = {
  image: "image/*",
  video: "video/*",
  audio: "audio/*",
};

/** 选项不多时用分段按钮，一眼看全；多了退回下拉 */
function EnumControl({
  field,
  value,
  disabled,
  invalid,
  onChange,
}: {
  field: SchemaField;
  value: unknown;
  disabled?: boolean;
  invalid?: boolean;
  onChange: (value: unknown) => void;
}) {
  const options = field.options ?? [];
  if (options.length <= 3) {
    return (
      <div
        role="radiogroup"
        aria-label={field.label}
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
              {option.label}
            </button>
          );
        })}
      </div>
    );
  }
  return (
    <select
      aria-label={field.label}
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
          {option.label}
        </option>
      ))}
    </select>
  );
}

/** 媒体字段：上传新文件，或直接选画布里已有的素材 */
function MediaControl({
  field,
  value,
  asset,
  disabled,
  invalid,
  onChange,
  listAssets,
}: {
  field: SchemaField & { type: "image" | "video" | "audio" };
  value: unknown;
  asset?: ParamAsset;
  disabled?: boolean;
  invalid?: boolean;
  onChange: (value: unknown, asset: ParamAsset | null) => void;
  listAssets: VideoParamPanelProps["listAssets"];
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [choices, setChoices] = useState<AssetChoice[]>([]);
  const hasValue = value !== undefined && value !== null && value !== "";

  const upload = async (file: File) => {
    setUploading(true);
    setUploadError(null);
    try {
      const uploaded = await uploadAsset(file);
      onChange(uploaded.id, {
        url: uploaded.url,
        fileName: uploaded.fileName ?? file.name,
        mediaType: field.type,
      });
    } catch {
      setUploadError("上传失败，请重试");
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="flex flex-col gap-1.5">
      {hasValue && (
        <div
          className={cn(
            "bg-muted/50 flex items-center gap-2 rounded-lg border p-1.5",
            invalid && "border-destructive/40",
          )}
        >
          {asset?.url && field.type === "image" && (
            <img src={asset.url} alt="" className="size-9 rounded object-cover" />
          )}
          {asset?.url && field.type === "video" && (
            <video src={asset.url} muted className="size-9 rounded object-cover" />
          )}
          <span className="min-w-0 flex-1 truncate text-xs">
            {asset?.fileName ?? `素材 #${String(value)}`}
          </span>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="清除素材"
            disabled={disabled}
            onClick={() => onChange(undefined, null)}
          >
            <X />
          </Button>
        </div>
      )}
      <div className="flex gap-1.5">
        <Button
          variant="outline"
          size="xs"
          disabled={disabled || uploading}
          onClick={() => inputRef.current?.click()}
        >
          {uploading ? <Loader2 className="animate-spin" /> : <Upload />}
          {hasValue ? "换一个" : "上传"}
        </Button>
        <DropdownMenu
          modal={false}
          onOpenChange={(open) => {
            if (open) setChoices(listAssets(field.type));
          }}
        >
          <DropdownMenuTrigger
            disabled={disabled}
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
                  画布里还没有可用的{field.label}
                </p>
              )}
              {choices.map((choice) => (
                <DropdownMenuItem
                  key={choice.assetId}
                  onClick={() =>
                    onChange(choice.assetId, {
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
      </div>
      <input
        ref={inputRef}
        type="file"
        accept={ACCEPT[field.type]}
        className="sr-only"
        aria-hidden
        tabIndex={-1}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) void upload(file);
        }}
      />
      {uploadError && <p className="text-destructive text-xs">{uploadError}</p>}
    </div>
  );
}

function FieldRow({
  field,
  params,
  asset,
  link,
  error,
  disabled,
  onChange,
  listAssets,
}: {
  field: SchemaField;
  params: Record<string, unknown>;
  asset?: ParamAsset;
  link?: IncomingLink;
  error?: string;
  disabled?: boolean;
  onChange: VideoParamPanelProps["onChange"];
  listAssets: VideoParamPanelProps["listAssets"];
}) {
  const value = effectiveValue(field, params, field.name);
  const invalid = !!error;
  const locked = !!link;

  let control;
  if (locked && isMediaFieldType(field.type)) {
    control = null;
  } else if (locked) {
    control = (
      <Input
        disabled
        value={link.text ?? ""}
        className="h-8 text-xs"
        aria-label={field.label}
      />
    );
  } else if (isMediaFieldType(field.type)) {
    control = (
      <MediaControl
        field={field as SchemaField & { type: "image" | "video" | "audio" }}
        value={value}
        asset={asset}
        disabled={disabled}
        invalid={invalid}
        listAssets={listAssets}
        onChange={(next, nextAsset) => onChange(field.name, next, nextAsset)}
      />
    );
  } else {
    switch (field.type) {
      case "enum":
        control = (
          <EnumControl
            field={field}
            value={value}
            disabled={disabled}
            invalid={invalid}
            onChange={(next) => onChange(field.name, next)}
          />
        );
        break;
      case "boolean":
        control = (
          <Switch
            size="sm"
            checked={value === true}
            disabled={disabled}
            aria-label={field.label}
            onCheckedChange={(checked) => onChange(field.name, checked)}
          />
        );
        break;
      case "number":
        control = (
          <Input
            type="number"
            inputMode="decimal"
            min={field.min}
            max={field.max}
            disabled={disabled}
            aria-invalid={invalid}
            aria-label={field.label}
            className="h-8 text-xs"
            value={value === undefined || value === null ? "" : String(value)}
            onChange={(event) => {
              const raw = event.target.value;
              onChange(field.name, raw === "" ? undefined : Number(raw));
            }}
          />
        );
        break;
      default:
        control = (
          <textarea
            rows={2}
            maxLength={field.max_length ? field.max_length * 2 : undefined}
            disabled={disabled}
            aria-invalid={invalid}
            aria-label={field.label}
            value={typeof value === "string" ? value : ""}
            className="nowheel border-input focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:border-destructive field-sizing-content max-h-24 min-h-14 w-full resize-none rounded-lg border bg-transparent px-2 py-1.5 text-xs outline-none focus-visible:ring-3 disabled:opacity-50 dark:bg-input/30"
            onChange={(event) => onChange(field.name, event.target.value)}
          />
        );
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="text-muted-foreground">
          {field.label}
          {field.required && <span className="text-destructive ml-0.5">*</span>}
        </span>
        {locked && (
          <span
            className="bg-muted text-muted-foreground inline-flex max-w-40 items-center gap-1 rounded-md px-1.5 py-0.5"
            title={`来自：${link.sourceLabel}`}
          >
            <Link2 className="size-3 shrink-0" />
            <span className="truncate">来自：{link.sourceLabel}</span>
          </span>
        )}
      </div>
      {control}
      {error && <p className="text-destructive text-xs">{error}</p>}
    </div>
  );
}

/**
 * 按模型 input_schema 渲染的参数面板：字段顺序就是 schema 的键顺序，
 * advanced 的收进「高级」；有 port 且已连线的字段显示「来自：某某节点」并禁用手填。
 * 提示词（prompt 字段）由输入框负责，这里不重复画。
 */
export function VideoParamPanel({
  schema,
  params,
  paramAssets,
  bindings,
  errors,
  showErrors,
  disabled,
  onChange,
  listAssets,
}: VideoParamPanelProps) {
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const fields = schemaFields(schema).filter(
    (field) => !(field.name === "prompt" && field.type === "text"),
  );
  const basic = fields.filter((field) => !field.advanced);
  const advanced = fields.filter((field) => field.advanced);
  const advancedHasError = showErrors && advanced.some((field) => errors[field.name]);
  const advancedVisible = advancedOpen || advancedHasError;

  if (fields.length === 0) return null;

  const renderField = (field: SchemaField) => (
    <FieldRow
      key={field.name}
      field={field}
      params={params}
      asset={paramAssets?.[field.name]}
      link={bindings[field.name]}
      error={showErrors ? errors[field.name] : undefined}
      disabled={disabled}
      onChange={onChange}
      listAssets={listAssets}
    />
  );

  return (
    <div className="nowheel flex max-h-72 flex-col gap-2.5 overflow-y-auto px-1 py-0.5">
      {basic.map(renderField)}
      {advanced.length > 0 && (
        <>
          <button
            type="button"
            aria-expanded={advancedVisible}
            className="text-muted-foreground hover:text-foreground flex items-center gap-1 self-start text-xs"
            onClick={() => setAdvancedOpen((open) => !open)}
          >
            <ChevronDown
              className={cn("size-3.5 transition-transform", advancedVisible && "rotate-180")}
            />
            高级（{advanced.length}）
          </button>
          {advancedVisible && advanced.map(renderField)}
        </>
      )}
    </div>
  );
}
