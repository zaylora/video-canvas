import {
  AudioLines,
  Image as ImageIcon,
  Loader2,
  Sparkles,
  Type,
  Video,
  type LucideIcon,
} from "lucide-react";

import type { Capabilities, GenerationOp, Pricing } from "@/api/model/type.d";
import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { Tag } from "@/components/admin-ui/tag";
import { VideoParamPanel } from "@/components/canvas/video-param-panel";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import type { ParamAsset } from "@/types";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { fanoutCount, quote } from "@/utils/pricing/quote";
import {
  buildTaskInput,
  currentOp,
  OP_LABEL,
  priceSpecOf,
  type RefKey,
} from "@/utils/tasks/capabilities";

const KIND_ICON: Record<string, LucideIcon> = {
  text: Type,
  image: ImageIcon,
  video: Video,
  audio: AudioLines,
};

/**
 * 模拟画布节点：按当前草稿的 capabilities 渲染，和画布上用户看到的一样可以直接选生成方式、参数、上传素材。
 * 提示词单独画成输入框，参考素材与生成参数交给画布同一个参数面板。
 * @param action 底部主按钮（预览里是“去测试”，测试弹窗里是“开始测试”）
 */
export function TestNode({
  modelKey,
  vendor,
  label,
  kind,
  pricing,
  caps,
  params,
  assets,
  errors,
  showErrors,
  disabled,
  onChange,
  onAddRef,
  onRemoveRef,
  action,
  className,
}: {
  modelKey: string;
  /** 厂商 slug，用来显示 logo */
  vendor?: string;
  label: string;
  kind: string;
  /** 定价，用来在按钮上显示本次的积分 */
  pricing?: Pricing;
  caps: Capabilities | undefined;
  params: Record<string, unknown>;
  assets: Record<string, ParamAsset>;
  errors: Record<string, string>;
  showErrors: boolean;
  disabled?: boolean;
  onChange: (name: string, value: unknown) => void;
  onAddRef: (key: RefKey, assetId: string | number, asset: ParamAsset) => void;
  onRemoveRef: (key: RefKey, assetId: string | number) => void;
  action?: { label: string; onClick: () => void; busy?: boolean; disabled?: boolean };
  className?: string;
}) {
  const Icon = KIND_ICON[kind] ?? Sparkles;
  const op: GenerationOp | undefined = currentOp(caps, params);
  const spec = priceSpecOf(caps, buildTaskInput(caps, params).input);
  const credits = pricing ? quote(pricing, caps, spec) * fanoutCount(caps, spec.params) : null;
  const promptValue = typeof params.prompt === "string" ? params.prompt : "";
  const promptError = showErrors ? errors.prompt : undefined;

  return (
    <div
      data-slot="test-node"
      className={cn("bg-card overflow-hidden rounded-xl border shadow-lg", className)}
    >
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <VendorAvatar
          vendor={vendor}
          name={label || "?"}
          seed={modelKey}
          className="size-6 text-[11px]"
        />
        <span className="truncate text-sm font-medium">{label || "未命名模型"}</span>
        {kind && (
          <Tag className="ml-auto">
            <Icon />
            {MODEL_KIND_LABEL[kind] ?? kind}
          </Tag>
        )}
      </div>

      <div className="flex flex-col gap-3 p-3">
        {!caps && (
          <p className="text-muted-foreground rounded-md border border-dashed p-3 text-center text-xs">
            capabilities 还没有配置，节点上没有可填的内容。
          </p>
        )}
        {caps && (
          <>
            {(caps.ops?.length ?? 0) > 1 && (
              <div
                role="radiogroup"
                aria-label="生成方式"
                className="bg-muted flex gap-0.5 rounded-lg p-0.5"
              >
                {caps.ops?.map((item) => (
                  <button
                    key={item}
                    type="button"
                    role="radio"
                    aria-checked={item === op}
                    disabled={disabled}
                    className={cn(
                      "min-w-0 flex-1 truncate rounded-md px-2 py-1 text-xs transition-colors disabled:opacity-50",
                      item === op
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                    onClick={() => onChange("op", item)}
                  >
                    {OP_LABEL[item]}
                  </button>
                ))}
              </div>
            )}
            <div className="flex flex-col gap-1">
              <Textarea
                aria-label="提示词"
                value={promptValue}
                disabled={disabled}
                aria-invalid={!!promptError}
                placeholder={kind === "audio" ? "输入要朗读的文本…" : "描述你想生成的内容…"}
                className="min-h-20 resize-none text-xs"
                onChange={(event) => onChange("prompt", event.target.value)}
              />
              <div className="flex text-[10px]">
                {promptError && <span className="text-destructive">{promptError}</span>}
                <span className="text-muted-foreground ml-auto tabular-nums">
                  {[...promptValue].length} / {caps.prompt?.max_length ?? 0}
                </span>
              </div>
            </div>
            <VideoParamPanel
              caps={caps}
              op={op}
              params={params}
              paramAssets={assets}
              bindings={{ images: [], videos: [], audios: [] }}
              errors={errors}
              showErrors={showErrors}
              disabled={disabled}
              onChange={onChange}
              onAddRef={onAddRef}
              onRemoveRef={onRemoveRef}
              listAssets={() => []}
            />
          </>
        )}
      </div>

      {action && (
        <div className="border-t p-3">
          <Button
            className="w-full"
            size="sm"
            disabled={action.disabled || action.busy}
            onClick={action.onClick}
          >
            {action.busy ? <Loader2 className="animate-spin" /> : <Sparkles />}
            {action.label}
            {credits !== null && ` · ${pricing?.billing === "token" ? "≤" : ""}${credits} 积分`}
          </Button>
        </div>
      )}
    </div>
  );
}
