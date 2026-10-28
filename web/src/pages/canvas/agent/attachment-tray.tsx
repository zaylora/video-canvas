import { EyeOff, RotateCw, X } from "lucide-react";

import { cn } from "@/lib/utils";
import type { TrayItem } from "@/utils/agent/attachments";

/** 进度环：上传中盖在缩略图上 */
function ProgressRing({ percent }: { percent: number }) {
  const r = 9;
  const c = 2 * Math.PI * r;
  return (
    <svg
      role="img"
      aria-label={`上传中 ${Math.round(percent)}%`}
      viewBox="0 0 22 22"
      className="size-[22px] -rotate-90"
    >
      <circle
        cx="11"
        cy="11"
        r={r}
        fill="none"
        stroke="currentColor"
        strokeOpacity={0.3}
        strokeWidth={2.5}
      />
      <circle
        cx="11"
        cy="11"
        r={r}
        fill="none"
        stroke="currentColor"
        strokeWidth={2.5}
        strokeLinecap="round"
        strokeDasharray={c}
        strokeDashoffset={c * (1 - Math.min(100, Math.max(0, percent)) / 100)}
      />
    </svg>
  );
}

/**
 * 输入框上方的图片附件托盘（设计稿「Agent聊天框改进」6.1.1）：一排 56×56 的缩略图。
 * 上传中盖进度环；失败描边、点图上的 ↻ 重试；悬停出现 × 移除（上传中移除就是取消）；
 * 当前模型不能看图时缩略图变暗，底部写明原因（发送按钮同时被禁用）。托盘空着不渲染。
 */
export function AttachmentTray({
  items,
  vision,
  onRetry,
  onRemove,
}: {
  items: TrayItem[];
  /** 当前模型能不能看图 */
  vision: boolean;
  onRetry: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  if (items.length === 0) return null;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex gap-1.5 overflow-x-auto pb-0.5">
        {items.map((item) => {
          const failed = item.status === "error";
          return (
            <div
              key={item.id}
              title={failed && item.error ? `上传失败：${item.error}` : item.name}
              className={cn(
                "group/tile border-chrome-border relative size-14 shrink-0 overflow-hidden rounded-lg border",
                failed && "border-status-warning ring-status-warning ring-1",
                !vision && item.status === "done" && "opacity-45",
              )}
            >
              <img
                src={item.previewUrl}
                alt={item.name}
                draggable={false}
                className="size-full object-cover"
              />
              {item.status === "uploading" && (
                <div className="absolute inset-0 grid place-items-center bg-black/40 text-white">
                  <ProgressRing percent={item.progress} />
                </div>
              )}
              {failed && (
                <button
                  type="button"
                  aria-label="重试上传"
                  onClick={() => onRetry(item.id)}
                  className="absolute inset-0 grid place-items-center bg-black/50 text-white outline-none focus-visible:ring-2 focus-visible:ring-white/70"
                >
                  <RotateCw className="size-4" />
                </button>
              )}
              <button
                type="button"
                aria-label={`移除 ${item.name}`}
                onClick={() => onRemove(item.id)}
                className="absolute top-0.5 right-0.5 grid size-[18px] place-items-center rounded-full bg-black/60 text-white opacity-0 transition-opacity duration-120 outline-none group-focus-within/tile:opacity-100 group-hover/tile:opacity-100 focus-visible:opacity-100"
              >
                <X className="size-3" />
              </button>
            </div>
          );
        })}
      </div>
      {!vision && (
        <div className="text-status-warning flex items-center gap-1.5 text-[11.5px]">
          <EyeOff aria-hidden className="size-3.5 shrink-0" />
          当前模型看不到图片，换一个能看图的模型或移除图片
        </div>
      )}
    </div>
  );
}
