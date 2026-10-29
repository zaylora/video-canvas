import { motion } from "motion/react";
import { Folder, Pencil, Trash2 } from "lucide-react";

import type { FolderDto } from "@/api/asset-library/type";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatAssetDay } from "@/utils/assets/format";

import { CardMenu } from "./card-menu";

/**
 * 个人资产库根目录里的文件夹卡：点击进入；⋯ 菜单里有「重命名」与「删除」。
 * 尺寸和素材卡（正方形）一致，网格里排得整齐。
 * @param folder 文件夹
 * @param onOpen 进入文件夹
 * @param onRename 菜单里的「重命名」；和 onDelete 都不给就没有 ⋯ 菜单（画布资产库弹框里只能进入）
 * @param onDelete 菜单里的「删除」，由调用方弹确认框
 * @param wide 资产页单独一排的文件夹用 16:10；默认正方形，和画布资产库弹框里的素材卡排齐
 */
export function FolderCard({
  folder,
  onOpen,
  onRename,
  onDelete,
  wide,
}: {
  wide?: boolean;
  folder: FolderDto;
  onOpen: () => void;
  onRename?: () => void;
  onDelete?: () => void;
}) {
  return (
    <motion.div
      data-slot="folder-card"
      whileTap={TAP_CARD}
      className={cn(
        "group/card bg-chrome-hover ring-chrome-border hover:bg-muted relative min-w-0 ring-1 transition-colors duration-150",
        wide ? "aspect-[16/10] rounded-2xl" : "aspect-square rounded-xl",
      )}
    >
      <button
        type="button"
        aria-label={`打开文件夹 ${folder.name}`}
        onClick={onOpen}
        className="focus-visible:ring-ring/60 absolute inset-0 flex flex-col items-center justify-center gap-2.5 rounded-[inherit] px-3 text-center outline-none focus-visible:ring-3"
      >
        <Folder className="text-muted-foreground size-1/4 min-w-8 stroke-[1.5]" />
        <span className="block w-full">
          <span className="block truncate text-sm font-medium">{folder.name}</span>
          <span className="text-muted-foreground block truncate text-xs tabular-nums">
            {folder.itemCount} 项 · {formatAssetDay(folder.createdAt)}
          </span>
        </span>
      </button>
      {onRename && onDelete && (
        <CardMenu>
          <DropdownMenuItem onClick={onRename}>
            <Pencil />
            重命名
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={onDelete}>
            <Trash2 />
            删除
          </DropdownMenuItem>
        </CardMenu>
      )}
    </motion.div>
  );
}
