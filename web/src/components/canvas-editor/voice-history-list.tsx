import { AlertCircle, Check, Loader2, Pause, Play } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import { ChromeButton } from "@/components/canvas/chrome/chrome";
import { toggleVoice, useVoicePlaying } from "@/components/characters/voice-player";
import { cn } from "@/lib/utils";
import type { VoiceHistoryRow } from "@/utils/characters/voice-history";

/** 一行的状态文字：生成中带进度，失败带原因，取消标明 */
function stateText(row: VoiceHistoryRow): string | null {
  switch (row.state.phase) {
    case "generating":
      return row.state.progress !== null ? `生成中 ${row.state.progress}%` : "生成中";
    case "failed":
      return row.state.reason;
    case "canceled":
      return "已取消";
    default:
      return null;
  }
}

/** 生成记录的一行：左边试听（或状态图标），中间提示词与模型、时间，右边「设为音色」或「当前」 */
function VoiceHistoryItem({
  row,
  onUse,
}: {
  /** 这一行 */
  row: VoiceHistoryRow;
  /** 设为音色 */
  onUse: (asset: { id: string; url: string }) => void;
}) {
  const asset = row.state.phase === "done" ? row.state.asset : null;
  const playing = useVoicePlaying(asset?.id);
  const status = stateText(row);
  return (
    <li
      className={cn(
        "flex min-h-11 items-center gap-2 rounded-2xl px-1.5 py-1 ring-1",
        row.current ? "bg-chrome-hover ring-chrome-border" : "ring-transparent",
      )}
    >
      {asset ? (
        <ChromeButton
          aria-label={playing ? "暂停" : "试听"}
          aria-pressed={playing}
          onClick={() => toggleVoice({ id: asset.id, url: asset.url } as AssetDto)}
        >
          {playing ? <Pause /> : <Play />}
        </ChromeButton>
      ) : (
        <span className="text-muted-foreground grid size-8 shrink-0 place-items-center">
          {row.state.phase === "generating" ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <AlertCircle
              className={cn("size-4", row.state.phase === "failed" && "text-destructive")}
            />
          )}
        </span>
      )}
      <div className="min-w-0 flex-1">
        {row.prompt && (
          <p className="truncate text-sm" title={row.prompt}>
            {row.prompt}
          </p>
        )}
        <p className="text-muted-foreground truncate text-xs">
          {row.model} · {row.createdAt}
        </p>
        {status && (
          <p
            className={cn(
              "truncate text-xs",
              row.state.phase === "failed" ? "text-destructive" : "text-muted-foreground",
            )}
            title={status}
          >
            {status}
          </p>
        )}
      </div>
      {row.current ? (
        <span className="text-foreground flex shrink-0 items-center gap-1 px-2 text-xs">
          <Check className="size-3.5" />
          当前
        </span>
      ) : (
        asset && (
          <ChromeButton className="text-xs" onClick={() => onUse(asset)}>
            设为音色
          </ChromeButton>
        )
      )}
    </li>
  );
}

/**
 * 详情页「AI 生成声音」下面的生成记录：这个角色卡节点在这张画布上最近的声音任务，新的在前。
 * 成功的可以试听、设为音色，当前声音那条标出来；失败的显示原因。没有记录时什么都不渲染。
 */
export function VoiceHistoryList({
  rows,
  onUse,
}: {
  /** 生成记录的行，新的在前 */
  rows: VoiceHistoryRow[];
  /** 把一条记录的音频设为音色 */
  onUse: (asset: { id: string; url: string }) => void;
}) {
  if (rows.length === 0) return null;
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-[13px] font-medium">生成记录</span>
      <ul className="bg-chrome ring-chrome-border flex max-h-64 flex-col gap-0.5 overflow-y-auto rounded-3xl p-1.5 ring-1 backdrop-blur-xl backdrop-saturate-150">
        {rows.map((item) => (
          <VoiceHistoryItem key={item.taskId} row={item} onUse={onUse} />
        ))}
      </ul>
    </div>
  );
}
