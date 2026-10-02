import { cn } from "@/lib/utils";
import { coverLayoutOf } from "@/utils/home/home";

/** 一个节点线框：标题条 + 两行正文 */
function WireNode({ x, y }: { x: number; y: number }) {
  return (
    <g>
      <rect x={x} y={y} width={60} height={36} rx={6} className="fill-muted stroke-chrome-border" />
      <rect
        x={x + 7}
        y={y + 8}
        width={22}
        height={3}
        rx={1.5}
        className="fill-muted-foreground/35"
      />
      <rect
        x={x + 7}
        y={y + 17}
        width={40}
        height={2.5}
        rx={1.2}
        className="fill-muted-foreground/35"
      />
      <rect
        x={x + 7}
        y={y + 24}
        width={30}
        height={2.5}
        rx={1.2}
        className="fill-muted-foreground/35"
      />
    </g>
  );
}

/** 两个节点之间的贝塞尔连线，和画布里的连线一个走法 */
function WireEdge({ from, to }: { from: [number, number]; to: [number, number] }) {
  const mid = (from[0] + to[0]) / 2;
  return (
    <path
      d={`M${from[0]} ${from[1]} C${mid} ${from[1]} ${mid} ${to[1]} ${to[0]} ${to[1]}`}
      className="stroke-edge fill-none"
      strokeWidth={1.2}
    />
  );
}

/** 三种布局：一对、链式三连、一分二。数量要和 utils/home 的 COVER_LAYOUT_COUNT 一致 */
const LAYOUTS = [
  <>
    <WireNode x={30} y={57} />
    <WireNode x={110} y={57} />
    <WireEdge from={[90, 75]} to={[110, 75]} />
  </>,
  <>
    <WireNode x={20} y={20} />
    <WireNode x={70} y={62} />
    <WireNode x={120} y={104} />
    <WireEdge from={[80, 38]} to={[70, 80]} />
    <WireEdge from={[130, 80]} to={[120, 122]} />
  </>,
  <>
    <WireNode x={25} y={62} />
    <WireNode x={115} y={22} />
    <WireNode x={115} y={102} />
    <WireEdge from={[85, 80]} to={[115, 40]} />
    <WireEdge from={[85, 80]} to={[115, 120]} />
  </>,
];

/**
 * 画布卡的封面：有封面图就铺图，没有就在画布同款点阵上画节点线框，
 * 布局按画布 id 固定，同一张画布每次看到的样子不变。
 */
export function CanvasCover({
  id,
  coverUrl,
  className,
}: {
  id: string;
  coverUrl: string | null;
  className?: string;
}) {
  if (coverUrl) {
    return (
      <img
        src={coverUrl}
        alt=""
        loading="lazy"
        draggable={false}
        className={cn("size-full object-cover", className)}
      />
    );
  }
  return (
    <div
      data-slot="canvas-cover"
      className={cn(
        "bg-canvas size-full bg-[radial-gradient(circle,var(--canvas-dot)_1px,transparent_1.3px)] bg-size-[14px_14px]",
        className,
      )}
    >
      <svg
        viewBox="0 0 200 150"
        preserveAspectRatio="xMidYMid slice"
        className="size-full"
        aria-hidden
      >
        {LAYOUTS[coverLayoutOf(id)]}
      </svg>
    </div>
  );
}
