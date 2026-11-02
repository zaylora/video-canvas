import { Check } from "lucide-react";

import type { ModelInfo } from "@/api/model/type";
import { ModelLogo } from "@/components/model-logo";
import { cn } from "@/lib/utils";
import { modelSummary, videoModelSupport } from "@/utils/drama/video-models";

import { ChipPopover } from "./pickers";

/** 一个模型在菜单里的说明：不能选时 ok 为 false，text 写原因 */
type Describe = (info: ModelInfo) => { ok: boolean; text: string };

/**
 * 模型选择菜单：前面是模型厂商 logo，下拉里每项带一行说明，不能选的置灰并写原因。
 * 短剧里的视频平台（新建、拆解、片场）和拆解时的图片模型共用。
 */
export function ModelMenu({
  models,
  loading,
  model,
  onPick,
  noun,
  title,
  describe,
  variant = "chip",
  size = "default",
  prefix,
}: {
  models: readonly ModelInfo[];
  loading: boolean;
  model: ModelInfo | undefined;
  onPick: (key: string) => void;
  /** 「视频」「图片」：拼进空状态文案 */
  noun: string;
  title?: string;
  describe: Describe;
  variant?: "chip" | "bar" | "chrome";
  size?: "default" | "lg";
  /** 按钮上模型名前的短标签，如「视频」 */
  prefix?: string;
}) {
  const name = model?.label ?? (loading ? "加载模型…" : `没有可用的${noun}模型`);
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
          {models.length === 0 && (
            <div className="text-muted-foreground px-2 py-3 text-sm">
              {loading ? "加载中…" : `暂无可用的${noun}模型`}
            </div>
          )}
          {models.map((info) => {
            const { ok, text } = describe(info);
            const active = model?.key === info.key;
            return (
              <button
                key={info.key}
                type="button"
                disabled={!ok}
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
                  {text && (
                    <span className="text-muted-foreground block truncate text-[11px] font-normal">
                      {text}
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

/** 视频模型的说明：只有收参考图的能选（片段里的 @ 资产要靠参考图带入） */
export const describeVideoModel: Describe = (info) => {
  const support = videoModelSupport(info);
  return support.ok
    ? { ok: true, text: modelSummary(info) }
    : { ok: false, text: support.reason ?? "" };
};

/** 图片模型的说明：都能选，写模型自带的简介 */
export const describeImageModel: Describe = (info) => ({ ok: true, text: info.hint ?? "" });

/** 片场编辑区生成按钮旁的视频模型选择 */
export function VideoModelMenu(props: {
  models: readonly ModelInfo[];
  loading: boolean;
  model: ModelInfo | undefined;
  onPick: (key: string) => void;
}) {
  return (
    <ModelMenu
      {...props}
      noun="视频"
      title="生成片段视频用的模型"
      describe={describeVideoModel}
      variant="bar"
      size="lg"
    />
  );
}
