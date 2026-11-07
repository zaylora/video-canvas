import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuRadioGroup,
  ContextMenuRadioItem,
  ContextMenuSeparator,
} from "@/components/ui/context-menu";
import type { PetMode } from "@/utils/pet/mood";

const CONTENT =
  "bg-chrome ring-chrome-border min-w-40 rounded-xl p-1.5 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150";
const ITEM = "focus:bg-chrome-hover h-8 rounded-lg px-2.5";

/** 桌宠的右键菜单：活泼 / 安静（单选）、回到右下角、隐藏桌宠；窄屏不能拖，没有「回到右下角」 */
export function PetMenuContent({
  mode,
  canReset,
  onMode,
  onReset,
  onHide,
}: {
  mode: PetMode;
  canReset: boolean;
  onMode: (mode: Exclude<PetMode, "hidden">) => void;
  onReset: () => void;
  onHide: () => void;
}) {
  return (
    <ContextMenuContent className={CONTENT} side="bottom" align="start" alignOffset={0}>
      <ContextMenuRadioGroup
        value={mode}
        onValueChange={(value) => onMode(value as Exclude<PetMode, "hidden">)}
      >
        <ContextMenuRadioItem value="lively" className={ITEM}>
          活泼
        </ContextMenuRadioItem>
        <ContextMenuRadioItem value="quiet" className={ITEM}>
          安静
        </ContextMenuRadioItem>
      </ContextMenuRadioGroup>
      <ContextMenuSeparator className="bg-chrome-border mx-0.5" />
      {canReset && (
        <ContextMenuItem className={ITEM} onClick={onReset}>
          回到右下角
        </ContextMenuItem>
      )}
      <ContextMenuItem className={ITEM} onClick={onHide}>
        隐藏桌宠
      </ContextMenuItem>
    </ContextMenuContent>
  );
}
