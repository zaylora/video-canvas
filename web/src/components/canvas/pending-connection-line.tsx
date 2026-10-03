import { getBezierPath, Position, type XYPosition } from "@xyflow/react";

/** handle 朝向对应的落点朝向，出入线方向才顺 */
const OPPOSITE_POSITION: Record<Position, Position> = {
  [Position.Left]: Position.Right,
  [Position.Right]: Position.Left,
  [Position.Top]: Position.Bottom,
  [Position.Bottom]: Position.Top,
};

type PendingConnectionLineProps = {
  /** 拉出端 handle 的屏幕坐标 */
  from: XYPosition;
  /** 拉出端 handle 的朝向 */
  fromPosition: Position;
  /** 松手落点的屏幕坐标 */
  to: XYPosition;
};

/**
 * 选种类期间补画的引导线：松手那刻 ReactFlow 就收走了自己的预览线，
 * 这里按屏幕坐标续上一条，让菜单看起来是挂在线头上的。
 */
export function PendingConnectionLine({ from, fromPosition, to }: PendingConnectionLineProps) {
  const [path] = getBezierPath({
    sourceX: from.x,
    sourceY: from.y,
    sourcePosition: fromPosition,
    targetX: to.x,
    targetY: to.y,
    targetPosition: OPPOSITE_POSITION[fromPosition],
  });

  return (
    // 盖在画布上、压在菜单下，纯装饰所以不吃指针事件
    <svg className="pointer-events-none fixed inset-0 z-40 size-full" aria-hidden>
      <path d={path} fill="none" strokeWidth={1} className="stroke-muted-foreground" />
    </svg>
  );
}

type PendingFanLinesProps = {
  /** 各来源节点出口的屏幕坐标 */
  froms: XYPosition[];
  /** 当前指针（或松手落点）的屏幕坐标，所有线汇到这里 */
  to: XYPosition;
};

/**
 * 多选引用时的扇入引导线：每个被引用的节点从右侧出口各拉一根线，汇到指针处。
 * 拖拽过程中和松手后选种类期间都用它，行为和单根的 PendingConnectionLine 一致。
 */
export function PendingFanLines({ froms, to }: PendingFanLinesProps) {
  return (
    <svg className="pointer-events-none fixed inset-0 z-40 size-full" aria-hidden>
      {froms.map((from, index) => {
        const [path] = getBezierPath({
          sourceX: from.x,
          sourceY: from.y,
          sourcePosition: Position.Right,
          targetX: to.x,
          targetY: to.y,
          targetPosition: Position.Left,
        });
        return (
          <path
            key={index}
            d={path}
            fill="none"
            strokeWidth={1}
            className="stroke-muted-foreground"
          />
        );
      })}
    </svg>
  );
}
