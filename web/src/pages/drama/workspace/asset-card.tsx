import { motion } from "motion/react";
import {
  AudioLines,
  Check,
  CircleX,
  Gem,
  Image as ImageIcon,
  Loader2,
  LocateFixed,
  Mountain,
  Sparkles,
  Type,
  UserRound,
  Video,
  type LucideIcon,
} from "lucide-react";

import { AssetCover } from "@/components/assets/asset-card";
import { DramaPoster } from "@/components/drama/poster";
import { Button } from "@/components/ui/button";
import type { CharacterEntry } from "@/store/characters";
import { DURATION, STAGGER, STAGGER_MAX, TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { AssetItem, AssetType } from "@/utils/drama/assets";

export const TYPE_ICON: Record<AssetType, LucideIcon> = {
  character: UserRound,
  scene: Mountain,
  prop: Gem,
  text: Type,
  image: ImageIcon,
  video: Video,
  audio: AudioLines,
};

/** 完成态的封面：角色取角色卡主图，其余取节点素材；没有可显示的就用渐变占位 */
function DoneCover({ item, entry }: { item: AssetItem; entry?: CharacterEntry }) {
  const { data } = item;
  if (item.type === "character") {
    const avatar = entry?.card?.avatar;
    if (avatar) return <AssetCover asset={avatar} />;
  }
  if (data.kind === "script") {
    return (
      <div className="text-muted-foreground bg-muted absolute inset-0 overflow-hidden p-3 text-xs leading-5 break-words">
        {data.text?.slice(0, 160)}
      </div>
    );
  }
  if (data.src && data.mediaType !== "video" && data.kind !== "video" && data.kind !== "audio") {
    return (
      <img
        src={data.src}
        alt={item.name}
        loading="lazy"
        draggable={false}
        className="absolute inset-0 size-full object-cover"
      />
    );
  }
  if (data.src && (data.kind === "video" || data.mediaType === "video")) {
    return (
      <video
        src={data.src}
        muted
        preload="metadata"
        className="absolute inset-0 size-full object-cover"
      />
    );
  }
  return <DramaPoster seed={item.name + item.look} className="absolute inset-0" />;
}

/** 空状态里的一块说明（待生成 / 生成中 / 失败） */
function EmptyFrame({
  tone = "idle",
  children,
}: {
  tone?: "idle" | "error";
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "absolute inset-0 grid place-items-center rounded-[inherit] border border-dashed p-2 text-center text-xs",
        tone === "error"
          ? "border-destructive/40 text-destructive"
          : "text-muted-foreground border-border",
      )}
    >
      <div className="flex flex-col items-center gap-1.5">{children}</div>
    </div>
  );
}

/**
 * 资产卡（原型资产列表）：封面按状态显示——已完成（素材）、生成中、分析设定中、失败、待生成（角色显示「待生成形象」）。
 * 悬停出「生成 / 重试」和「画布」（在画布中定位）；多选模式下左上角出勾选框。
 * 出现集数：全集一个小标，具体集号一串小点，未分类写「未分类」。
 */
export function AssetCard({
  item,
  index,
  entry,
  multi,
  selected,
  generating,
  onToggle,
  onGenerate,
  onLocate,
}: {
  item: AssetItem;
  index: number;
  entry?: CharacterEntry;
  multi: boolean;
  selected: boolean;
  /** 能不能生成（角色 / 场景 / 道具，且不是正在生成） */
  generating: boolean;
  onToggle: () => void;
  onGenerate: () => void;
  onLocate: () => void;
}) {
  const Icon = TYPE_ICON[item.type];
  const canGenerate =
    (item.type === "character" || item.type === "scene" || item.type === "prop") &&
    (item.status === "idle" || item.status === "error");
  const hasVoice = item.type === "character" && !!entry?.card?.voicePrompt;

  return (
    <motion.div
      data-slot="asset-card"
      layout="position"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: DURATION.base, delay: Math.min(index, STAGGER_MAX) * STAGGER }}
      className="group/card min-w-0"
    >
      <motion.div
        whileTap={multi && canGenerate ? TAP_CARD : undefined}
        className={cn(
          "bg-muted relative aspect-3/4 overflow-hidden rounded-xl",
          selected && "ring-primary ring-2",
        )}
      >
        {item.status === "done" ? (
          <>
            <DoneCover item={item} entry={entry} />
            <div className="from-cover-scrim absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-8 pb-2">
              <div className="text-cover-foreground truncate text-xs font-semibold">
                {item.name}
              </div>
            </div>
          </>
        ) : item.status === "running" ? (
          <EmptyFrame>
            <Loader2 className="size-5 animate-spin" />
            生成中
          </EmptyFrame>
        ) : item.status === "analyzing" ? (
          <EmptyFrame>
            <Loader2 className="size-5 animate-spin" />
            正在分析设定
          </EmptyFrame>
        ) : item.status === "error" ? (
          <EmptyFrame tone="error">
            <CircleX className="size-5" />
            生成失败
            <span className="text-muted-foreground text-[11px]">
              {item.data.error ? item.data.error.slice(0, 24) : "可以重试"}
            </span>
          </EmptyFrame>
        ) : (
          <EmptyFrame>
            <Icon className="size-5" />
            {item.type === "character" ? "待生成形象" : "待生成"}
          </EmptyFrame>
        )}

        {multi && canGenerate && (
          <button
            type="button"
            aria-label={selected ? "取消选择" : "选择"}
            aria-pressed={selected}
            onClick={onToggle}
            className={cn(
              "focus-visible:ring-ring/60 absolute inset-0 z-10 flex items-start justify-start p-2 outline-none focus-visible:ring-3 focus-visible:ring-inset",
            )}
          >
            <span
              className={cn(
                "bg-background/90 grid size-5 place-items-center rounded-md border",
                selected && "bg-primary text-primary-foreground border-transparent",
              )}
            >
              {selected && <Check className="size-3.5" />}
            </span>
          </button>
        )}

        {!multi && (
          <div className="absolute inset-0 z-10 flex items-center justify-center gap-1.5 bg-black/35 opacity-0 transition-opacity duration-150 group-focus-within/card:opacity-100 group-hover/card:opacity-100 max-md:opacity-100 max-md:bg-transparent max-md:items-end max-md:pb-9">
            {canGenerate && (
              <Button size="sm" disabled={generating} onClick={onGenerate}>
                <Sparkles />
                {item.status === "error" ? "重试" : "生成"}
              </Button>
            )}
            <Button size="sm" variant="secondary" onClick={onLocate}>
              <LocateFixed />
              画布
            </Button>
          </div>
        )}
      </motion.div>

      <div className="px-0.5 pt-1.5">
        <div className="truncate text-[13px] font-medium">
          {item.name}
          {item.look && <span className="text-muted-foreground font-normal"> · {item.look}</span>}
        </div>
        <div className="text-muted-foreground flex items-center gap-1.5 text-[11px]">
          {hasVoice && <AudioLines className="size-3" aria-label="已设定音色" />}
          {item.episodes === null ? (
            <span className="text-status-warning">未分类</span>
          ) : item.episodes.length === 0 ? (
            <span className="bg-muted rounded-full px-1.5 leading-4">全集</span>
          ) : (
            <span className="flex flex-wrap gap-0.5">
              {item.episodes.map((no) => (
                <i
                  key={no}
                  className="bg-muted min-w-4 rounded px-1 text-center leading-4 tabular-nums not-italic"
                >
                  {no}
                </i>
              ))}
            </span>
          )}
        </div>
      </div>
    </motion.div>
  );
}
