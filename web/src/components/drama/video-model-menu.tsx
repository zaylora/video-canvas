import { Check } from "lucide-react";

import type { ModelInfo } from "@/api/model/type";
import { ModelLogo } from "@/components/model-logo";
import { cn } from "@/lib/utils";
import { modelSummary, videoModelSupport } from "@/utils/drama/video-models";

import { ChipPopover } from "./pickers";

/**
 * 视频模型选择（片场编辑区生成按钮旁）：前面是模型厂商 logo；只有收参考图的模型可选，
 * 不支持的置灰并写原因（片段里的 @ 资产要靠参考图带入）。
 */
export function VideoModelMenu({
  models,
  loading,
  model,
  onPick,
}: {
  models: readonly ModelInfo[];
  loading: boolean;
  model: ModelInfo | undefined;
  onPick: (key: string) => void;
}) {
  return (
    <ChipPopover
      variant="bar"
      size="lg"
      icon={
        model && (
          <ModelLogo vendor={model.vendor} name={model.label} className="size-5 rounded-md" />
        )
      }
      label={model?.label ?? (loading ? "加载模型…" : "没有可用的视频模型")}
      title="生成片段视频用的模型"
      width="w-80"
    >
      {(close) => (
        <>
          {models.length === 0 && (
            <div className="text-muted-foreground px-2 py-3 text-sm">
              {loading ? "加载中…" : "暂无可用的视频模型"}
            </div>
          )}
          {models.map((info) => {
            const support = videoModelSupport(info);
            const active = model?.key === info.key;
            return (
              <button
                key={info.key}
                type="button"
                disabled={!support.ok}
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
                  <span className="text-muted-foreground block truncate text-[11px] font-normal">
                    {support.ok ? modelSummary(info) : support.reason}
                  </span>
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
