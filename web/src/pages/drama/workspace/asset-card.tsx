import { motion } from "motion/react";
import {
  AudioLines,
  Check,
  CircleX,
  Gem,
  Image as ImageIcon,
  Loader2,
  Mountain,
  PencilLine,
  Type,
  UserRound,
  Video,
  type LucideIcon,
} from "lucide-react";

import { DramaPoster } from "@/components/drama/poster";
import { Button } from "@/components/ui/button";
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

/** 完成态的封面：取节点素材（角色卡节点的主图也是它的 src）；没有可显示的就用渐变占位 */
function DoneCover({ item }: { item: AssetItem }) {
  const { data } = item;
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
        // 按图片原比例完整显示：三视图是横图，裁成 3:4 会只剩半个人；空出的部分露出卡片底色
        className="absolute inset-0 size-full object-contain"
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
 * 点整张卡进画布定位到对应节点；名字旁的按钮弹框编辑提示词（角色卡还能打开详情改音色）；
 * 多选 / 框选后批量生成，已生成的也能选上重新生成；多选模式下点卡片是勾选。
 * 根节点带 data-asset-id，列表框选按它量卡片位置。
 * 出现集数：全集一个小标，具体集号一串小点，未分类写「未分类」。
 */
export function AssetCard({
  item,
  index,
  multi,
  selected,
  onToggle,
  onLocate,
  onEditPrompt,
  onEditCharacter,
}: {
  item: AssetItem;
  index: number;
  multi: boolean;
  selected: boolean;
  onToggle: () => void;
  onLocate: () => void;
  /** 弹框编辑提示词、模型和参数（角色 / 场景 / 道具） */
  onEditPrompt: () => void;
  /** 打开角色卡详情（角色名、形象、补充图、音色） */
  onEditCharacter: () => void;
}) {
  const Icon = TYPE_ICON[item.type];
  const generated = item.type === "character" || item.type === "scene" || item.type === "prop";
  // 已生成的也能勾选：再生成一次就是重新生成
  const canGenerate =
    generated && (item.status === "idle" || item.status === "error" || item.status === "done");
  const hasVoice = item.type === "character" && !!item.data.character?.voice;

  return (
    <motion.div
      data-slot="asset-card"
      data-asset-id={item.id}
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
            <DoneCover item={item} />
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
          <button
            type="button"
            aria-label={`在画布中打开 ${item.name}`}
            title="在画布中打开"
            onClick={onLocate}
            className="focus-visible:ring-ring/60 absolute inset-0 z-10 cursor-pointer rounded-[inherit] outline-none transition-colors duration-150 hover:bg-white/5 focus-visible:ring-3 focus-visible:ring-inset"
          />
        )}
      </motion.div>

      <div className="px-0.5 pt-1.5">
        <div className="flex items-center gap-1">
          <div className="min-w-0 flex-1 truncate text-[13px] font-medium">
            {item.name}
            {item.look && <span className="text-muted-foreground font-normal"> · {item.look}</span>}
          </div>
          {generated && (
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={`编辑 ${item.name} 的提示词`}
              title="提示词与生成设置"
              onClick={onEditPrompt}
            >
              <PencilLine />
            </Button>
          )}
          {item.type === "character" && (
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={`编辑 ${item.name} 的角色详情与音色`}
              title="角色详情与音色"
              onClick={onEditCharacter}
            >
              <AudioLines />
            </Button>
          )}
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
