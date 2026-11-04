import { Check } from "lucide-react";

import type { ModelInfo } from "@/api/model/type";
import { ModelLogo } from "@/components/model-logo";
import { cn } from "@/lib/utils";
import { modelSummary, videoModelSupport } from "@/utils/drama/video-models";

import { ChipPopover } from "./pickers";

/** 模型能不能选：不能选时写原因（置灰显示） */
type Support = (info: ModelInfo) => { ok: boolean; reason: string | null };

/** 都能选 */
const anyModel: Support = () => ({ ok: true, reason: null });

/**
 * 模型选择胶囊（短剧里选视频 / 图片模型共用）：前面是厂商 logo，每项下面一行副文案，
 * 不能选的置灰并写原因。传了 defaultLabel 时列表顶部多一项「不指定」，选它回调空串。
 */
export function ModelMenu({
  models,
  loading,
  model,
  onPick,
  support = anyModel,
  summary,
  kindName,
  variant = "bar",
  size,
  prefix,
  defaultLabel,
  title,
}: {
  models: readonly ModelInfo[];
  loading: boolean;
  /** 当前选中的模型；undefined = 没选（显示 defaultLabel 或「没有可用的…」） */
  model: ModelInfo | undefined;
  onPick: (key: string) => void;
  support?: Support;
  /** 每项的副文案；不传没有 */
  summary?: (info: ModelInfo) => string;
  /** 模型种类名，用于空状态文案，如「视频」「图片」 */
  kindName: string;
  variant?: "chip" | "bar" | "chrome" | "field";
  size?: "default" | "lg";
  /** 胶囊文字前缀，如「人物」 */
  prefix?: string;
  /** 「不指定」项的文案（如「跟随默认」）；不传则没有这一项 */
  defaultLabel?: string;
  title?: string;
}) {
  const name =
    model?.label ??
    (defaultLabel && !loading ? defaultLabel : loading ? "加载模型…" : `没有可用的${kindName}模型`);
  return (
    <ChipPopover
      variant={variant}
      size={size}
      icon={
        model && (
          <ModelLogo vendor={model.vendor} name={model.label} className="size-5 rounded-md" />
        )
      }
      label={prefix ? `${prefix} · ${name}` : name}
      title={title}
      width="w-80"
    >
      {(close) => (
        <>
          {defaultLabel && (
            <button
              type="button"
              onClick={() => {
                close();
                onPick("");
              }}
              className={cn(
                "hover:bg-muted focus-visible:ring-ring/50 flex min-h-10 items-center gap-2 rounded-md px-2 py-1 text-left text-sm outline-none focus-visible:ring-2",
                !model && "bg-muted font-medium",
              )}
            >
              <span className="min-w-0 flex-1 truncate">{defaultLabel}</span>
              {!model && <Check className="size-4 shrink-0" />}
            </button>
          )}
          {models.length === 0 && (
            <div className="text-muted-foreground px-2 py-3 text-sm">
              {loading ? "加载中…" : `暂无可用的${kindName}模型`}
            </div>
          )}
          {models.map((info) => {
            const ability = support(info);
            const active = model?.key === info.key;
            const note = ability.ok ? summary?.(info) : ability.reason;
            return (
              <button
                key={info.key}
                type="button"
                disabled={!ability.ok}
                title={ability.reason ?? undefined}
                onClick={() => {
                  close();
                  onPick(info.key);
                }}
                className={cn(
                  "hover:bg-muted focus-visible:ring-ring/50 flex min-h-10 items-center gap-2 rounded-md px-2 py-1 text-left text-sm outline-none focus-visible:ring-2",
                  "disabled:hover:bg-transparent disabled:cursor-not-allowed disabled:opacity-45",
                  active && "bg-muted font-medium",
                )}
              >
                <ModelLogo vendor={info.vendor} name={info.label} className="size-5" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{info.label}</span>
                  {note && (
                    <span className="text-muted-foreground block truncate text-[11px] font-normal">
                      {note}
                    </span>
                  )}
                </span>
                {active && <Check className="size-4 shrink-0" />}
              </button>
            );
          })}
        </>
      )}
    </ChipPopover>
  );
}

/**
 * 视频模型选择（片场编辑区生成按钮旁，开拍设置里也用）：只有收参考图的模型可选，
 * 不支持的置灰并写原因（片段里的 @ 资产要靠参考图带入）；副文案是能力摘要（含时长上限）。
 */
export function VideoModelMenu({
  models,
  loading,
  model,
  onPick,
  variant = "bar",
  defaultLabel,
}: {
  models: readonly ModelInfo[];
  loading: boolean;
  model: ModelInfo | undefined;
  onPick: (key: string) => void;
  variant?: "chip" | "bar" | "chrome" | "field";
  /** 开拍设置用：多一项「不指定」 */
  defaultLabel?: string;
}) {
  return (
    <ModelMenu
      models={models}
      loading={loading}
      model={model}
      onPick={onPick}
      support={videoModelSupport}
      summary={modelSummary}
      kindName="视频"
      variant={variant}
      size={variant === "bar" ? "lg" : undefined}
      defaultLabel={defaultLabel}
      title={variant === "bar" ? "生成片段视频用的模型" : undefined}
    />
  );
}
