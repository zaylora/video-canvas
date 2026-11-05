import { cn } from "@/lib/utils";
import { ratioFrame, ratioIconBox } from "@/utils/ratio-icon";

/** Auto 的四角取景框（画在 20×20 里再缩放进图标）：和 1:1 的方框区分开 */
const AUTO_CORNERS = "M3 7V3h4 M13 3h4v4 M17 13v4h-4 M7 17H3v-4";

/**
 * 比例小图标：按真实宽高比画一个框，所有比例短边一样长（和 1:1 一样大）、长边按比例伸长，
 * 图标高度一致，放在一排时下面的文字对齐。
 * 画布生成面板、后台测试节点、短剧比例选择共用这一个，画法一致。
 * @param ratio 比例值，如 16:9；Auto 等非比例值画四角取景框
 * @param className 外层 svg 的样式，颜色跟随 currentColor
 */
export function RatioIcon({ ratio, className }: { ratio: string; className?: string }) {
  const frame = ratioFrame(ratio);
  const box = ratioIconBox(ratio);
  return (
    <svg
      aria-hidden
      viewBox={frame ? `0 0 ${box.width} ${box.height}` : "0 0 20 20"}
      width={box.width}
      height={box.height}
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
