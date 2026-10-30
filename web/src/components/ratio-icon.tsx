import { cn } from "@/lib/utils";
import { RATIO_ICON_BOX, ratioFrame } from "@/utils/ratio-icon";

/** Auto 的四角取景框：和 1:1 的方框区分开 */
const AUTO_CORNERS = "M3 7V3h4 M13 3h4v4 M17 13v4h-4 M7 17H3v-4";

/**
 * 比例小图标：按真实宽高比画一个框，所有比例同样大的外框、长边一样长，放在一排能直接比形状。
 * 画布生成面板、后台测试节点、短剧比例选择共用这一个，画法一致。
 * @param ratio 比例值，如 16:9；Auto 等非比例值画四角取景框
 * @param className 外层 svg 的样式，颜色跟随 currentColor
 */
export function RatioIcon({ ratio, className }: { ratio: string; className?: string }) {
  const frame = ratioFrame(ratio);
  return (
    <svg
      aria-hidden
      viewBox={`0 0 ${RATIO_ICON_BOX} ${RATIO_ICON_BOX}`}
      width={RATIO_ICON_BOX}
      height={RATIO_ICON_BOX}
      fill="none"
      stroke="currentColor"
      className={cn("shrink-0", className)}
    >
      {frame ? (
        <rect
          x={frame.x + frame.stroke / 2}
          y={frame.y + frame.stroke / 2}
          width={frame.width - frame.stroke}
          height={frame.height - frame.stroke}
          rx={Math.max(0, frame.radius - frame.stroke / 2)}
          strokeWidth={frame.stroke}
        />
      ) : (
        <path d={AUTO_CORNERS} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" />
      )}
    </svg>
  );
}
