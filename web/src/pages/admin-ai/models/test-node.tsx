import {
  AudioLines,
  Image as ImageIcon,
  Loader2,
  Sparkles,
  Type,
  Video,
  type LucideIcon,
} from "lucide-react";

import type { InputSchema } from "@/api/model/type";
import { InitialAvatar } from "@/components/admin-ui/initial-avatar";
import { Tag } from "@/components/admin-ui/tag";
import { VideoParamPanel } from "@/components/canvas/video-param-panel";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import type { ParamAsset } from "@/types";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";

const KIND_ICON: Record<string, LucideIcon> = {
  text: Type,
  image: ImageIcon,
  video: Video,
  audio: AudioLines,
};

/**
 * 模拟画布节点：按当前草稿的 input_schema 渲染，和画布上用户看到的一样可以直接选参数、上传素材。
 * 提示词（prompt 文本字段）单独画成输入框，其余字段交给画布同一个参数面板。
 * @param action 底部主按钮（预览里是“去测试”，测试弹窗里是“开始测试”）
 */
export function TestNode({
  modelKey,
  label,
  kind,
  credits,
  schema,
  params,
  assets,
  errors,
  showErrors,
  disabled,
  onChange,
  action,
  className,
}: {
  modelKey: string;
  label: string;
  kind: string;
  credits: number | null;
  schema: InputSchema | undefined;
  params: Record<string, unknown>;
  assets: Record<string, ParamAsset>;
  errors: Record<string, string>;
  showErrors: boolean;
  disabled?: boolean;
  onChange: (name: string, value: unknown, asset?: ParamAsset | null) => void;
  action?: { label: string; onClick: () => void; busy?: boolean; disabled?: boolean };
  className?: string;
}) {
  const Icon = KIND_ICON[kind] ?? Sparkles;
  const prompt = schema?.prompt?.type === "text" ? schema.prompt : undefined;
  const promptValue = typeof params.prompt === "string" ? params.prompt : "";
  const promptError = showErrors ? errors.prompt : undefined;
  const hasFields = !!schema && Object.keys(schema).length > 0;

  return (
    <div
      data-slot="test-node"
      className={cn("bg-card overflow-hidden rounded-xl border shadow-lg", className)}
    >
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <InitialAvatar name={label || "?"} seed={modelKey} className="size-6 text-[11px]" />
        <span className="truncate text-sm font-medium">{label || "未命名模型"}</span>
        {kind && (
          <Tag className="ml-auto">
            <Icon />
            {MODEL_KIND_LABEL[kind] ?? kind}
          </Tag>
        )}
      </div>

      <div className="flex flex-col gap-3 p-3">
        {!hasFields && (
          <p className="text-muted-foreground rounded-md border border-dashed p-3 text-center text-xs">
            input_schema 里还没有字段，节点上没有可填的参数。
          </p>
        )}
        {prompt && (
          <div className="flex flex-col gap-1">
            <Textarea
              aria-label={prompt.label}
              value={promptValue}
              disabled={disabled}
              aria-invalid={!!promptError}
              placeholder={kind === "audio" ? "输入要朗读的文本…" : "描述你想生成的内容…"}
              className="min-h-20 resize-none text-xs"
              onChange={(event) => onChange("prompt", event.target.value)}
            />
            <div className="flex text-[10px]">
              {promptError && <span className="text-destructive">{promptError}</span>}
              {prompt.max_length && (
                <span className="text-muted-foreground ml-auto tabular-nums">
                  {[...promptValue].length} / {prompt.max_length}
                </span>
              )}
            </div>
          </div>
        )}
        {schema && (
          <VideoParamPanel
            schema={schema}
            params={params}
            paramAssets={assets}
            bindings={{}}
            errors={errors}
            showErrors={showErrors}
            disabled={disabled}
            onChange={onChange}
            listAssets={() => []}
          />
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
            {credits !== null && ` · ${credits} 积分`}
          </Button>
        </div>
      )}
    </div>
  );
}
