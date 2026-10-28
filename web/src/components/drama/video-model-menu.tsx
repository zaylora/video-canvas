import { Check, Clapperboard } from "lucide-react";

import type { ModelInfo } from "@/api/model/type";
import { cn } from "@/lib/utils";
import { modelSummary, videoModelSupport } from "@/utils/drama/video-models";

import { ChipPopover } from "./pickers";

/**
 * 视频模型选择（分集页和片段工作区顶栏共用）：只有收参考图的模型可选，
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
      icon={<Clapperboard />}
      label={model?.label ?? (loading ? "加载模型…" : "没有可用的视频模型")}
      title="生成片段视频用的模型"
      width="w-80"
    >
      {(close) => (
        <>
          <div className="text-muted-foreground px-1.5 pt-1 text-xs">视频模型 · 片段要用参考图</div>
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
                <Clapperboard className="size-4 shrink-0" />
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
