import { motion } from "motion/react";
import { Pencil, Trash2, UserRound, VolumeX } from "lucide-react";

import type { CharacterDto } from "@/api/character/type";
import { AssetCover } from "@/components/assets/asset-card";
import { CardMenu } from "@/components/assets/card-menu";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";

import { VoiceBar } from "./voice-player";

/** 封面左上叠放的补充图缩略图最多几张 */
const STACK_MAX = 3;

/** 3:4 竖版封面：主图；主图素材已不可用时给人形占位，仍可点进去重选 */
export function CharacterCover({ card }: { card: CharacterDto }) {
  if (card.avatar) return <AssetCover asset={card.avatar} />;
  return (
    <div className="bg-muted text-muted-foreground/60 absolute inset-0 grid place-items-center">
      <UserRound className="size-1/4" />
    </div>
  );
}

/** 封面左上叠放的补充图缩略图（最多 3 张），后一张压在前一张上 */
export function ExtraStack({ card }: { card: CharacterDto }) {
  if (card.extras.length === 0) return null;
  return (
    <span
      data-slot="extra-stack"
      title={`${card.extras.length} 张补充图`}
      className="pointer-events-none absolute top-2 left-2 z-10 flex"
    >
      {card.extras.slice(0, STACK_MAX).map((asset, index) => (
        <span
          key={asset.id}
          className={cn(
            "bg-muted ring-background relative size-7 overflow-hidden rounded-md ring-2",
            index > 0 && "-ml-2.5",
          )}
        >
          <AssetCover asset={asset} />
        </span>
      ))}
    </span>
  );
}

/**
 * 资产页角色卡网格里的一张卡（设计 §6.4）：3:4 竖版封面（主图）、左上叠放补充图缩略图、
 * 底部渐变遮罩里是名称 + 声音试听条（没有声音显示「未绑定声音」）、右上「⋯」菜单（编辑 / 删除）。
 * 点封面直接打开编辑。
 * @param card 角色卡
 * @param onEdit 点封面或菜单「编辑」
 * @param onDelete 菜单「删除」
 */
export function CharacterCard({
  card,
  onEdit,
  onDelete,
}: {
  card: CharacterDto;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <motion.div
      data-slot="character-card"
      whileTap={TAP_CARD}
      className="group/card bg-muted relative aspect-3/4 min-w-0 overflow-hidden rounded-xl"
    >
      <CharacterCover card={card} />
      <button
        type="button"
        aria-label={`编辑 ${card.name}`}
        onClick={onEdit}
        className="focus-visible:ring-ring/60 absolute inset-0 z-0 rounded-[inherit] outline-none focus-visible:ring-3 focus-visible:ring-inset"
      />
      <ExtraStack card={card} />
      <div className="from-cover-scrim text-cover-foreground pointer-events-none absolute inset-x-0 bottom-0 z-10 bg-linear-to-t to-transparent px-2.5 pt-10 pb-2.5">
        <div className="truncate text-sm font-medium">{card.name}</div>
        <div className="mt-1.5 flex h-6 items-center text-xs">
          {card.voice ? (
            <VoiceBar asset={card.voice} onCover className="pointer-events-auto w-full" />
          ) : (
            <span className="flex items-center gap-1 opacity-70">
              <VolumeX className="size-3.5" />
              未绑定声音
            </span>
          )}
        </div>
      </div>
      <CardMenu>
        <DropdownMenuItem onClick={onEdit}>
          <Pencil />
          编辑
        </DropdownMenuItem>
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2 />
          删除
        </DropdownMenuItem>
      </CardMenu>
    </motion.div>
  );
}
