import { memo, type AnimationEventHandler } from "react";

import { cn } from "@/lib/utils";
import { PET_CELL } from "@/utils/pet/rows";

import type { PetAssets } from "./use-pet-assets";

/** 桌宠显示宽度：桌面 80px，窄屏 56px */
export const PET_WIDTH = { desktop: 80, narrow: 56 } as const;
/** 按宽度算出显示高度（保持 192:208 的比例） */
export const petHeight = (width: number) => Math.round((width * PET_CELL.height) / PET_CELL.width);

/**
 * 雪碧图元素：按 192×208 原尺寸画，再整体 scale 到显示宽度，避免 background-size 取小数时帧间出缝。
 * 帧动画全部交给 CSS（data-row 选 @keyframes）；React 只在动作行变化时渲染一次。
 * - data-loop="false"：只播一轮并停在最后一帧；
 * - data-paused="true"：页面在后台时暂停；
 * - sleepFallback：图集没有 sleeping 行，用 idle 第一帧（不写 data-row 就不播动画）降透明度，再叠一个 CSS 画的「z」。
 * 暗色主题给黑猫描一圈浅边（滤镜放在外层，在缩放后的真实像素上生效）。
 */
export const PetSprite = memo(function PetSprite({
  assets,
  row,
  loop,
  paused,
  sleepFallback,
  width,
  onAnimationEnd,
}: {
  assets: PetAssets;
  row: number;
  loop: boolean;
  paused: boolean;
  sleepFallback: boolean;
  width: number;
  onAnimationEnd: AnimationEventHandler<HTMLDivElement>;
}) {
  const scale = width / PET_CELL.width;
  return (
    <span
      className="animate-in fade-in-0 pointer-events-none relative block duration-300 motion-reduce:animate-none dark:[filter:drop-shadow(0_0_1px_hsl(0_0%_100%/.7))]"
      style={{ width, height: petHeight(width) }}
    >
      <span
        className={cn("pet-sprite absolute top-0 left-0 block", sleepFallback && "opacity-50")}
        data-row={sleepFallback ? undefined : row}
        data-loop={loop ? undefined : "false"}
        data-paused={paused ? "true" : undefined}
        onAnimationEnd={onAnimationEnd}
        style={{
          width: PET_CELL.width,
          height: PET_CELL.height,
          backgroundImage: `url("${assets.sheetUrl}")`,
          backgroundRepeat: "no-repeat",
          backgroundSize: `${assets.sheet.width}px ${assets.sheet.height}px`,
          backgroundPosition: "0 0",
          transform: `scale(${scale})`,
          transformOrigin: "0 0",
        }}
      />
      {sleepFallback && (
        <span
          aria-hidden
          className="text-muted-foreground absolute -top-1 right-1 animate-pulse text-sm font-semibold motion-reduce:animate-none"
        >
          z
        </span>
      )}
    </span>
  );
});
