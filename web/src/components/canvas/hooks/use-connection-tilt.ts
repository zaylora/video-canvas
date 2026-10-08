import { useEffect, useRef } from "react";
import { useNodeId, useStoreApi, type HandleType } from "@xyflow/react";
import { useReducedMotion, useSpring, useTransform, type MotionValue } from "motion/react";

import { tiltPerspective } from "@/utils/canvas/connection-tilt";

import { getNodeHit } from "../node-hit-test";

/** 最大倾斜角度：只要一眼看得出节点“偏过头去看鼠标”，再大就像要飞走了 */
const MAX_TILT = 7;

/** 沉下去的同时缩一丁点，像被鼠标按进画布里 */
const HOVER_SCALE = 0.985;

/** 透视距离，越小翻折感越强；节点宽 288px，900 左右刚好是“一点点” */
export const TILT_PERSPECTIVE = 900;

const SPRING = { stiffness: 260, damping: 26, mass: 0.6 } as const;

/** 拉过来的这根线的来头：从哪个节点、哪一端拉出来的 */
export type IncomingConnection = {
  nodeId: string;
  handleType: HandleType;
};

export type ConnectionTiltOptions = {
  /** 返回 false 就不给倾斜反馈，留给调用方按连接规则决定这根线接不接得上 */
  canAccept?: (from: IncomingConnection) => boolean;
};

export type ConnectionTilt = {
  rotateX: MotionValue<number>;
  rotateY: MotionValue<number>;
  scale: MotionValue<number>;
  /** 别人拉线时，本节点接不上就淡下去，一眼看出该往哪儿连 */
  opacity: MotionValue<number>;
  /** 透视距离：只在倾斜时有值，静止时为 0，免得每个节点都变成 3D 合成层 */
  transformPerspective: MotionValue<number>;
};

/** 拉线时接不上的节点淡到多少 */
const DIMMED_OPACITY = 0.35;

/**
 * 别人从另一个节点拉线过来、线头停在本节点身上时，让节点被指到的那一侧往里沉一点，
 * 像连线把它按进了画布，松手就能接上。
 *
 * 判定不走 connection.toNode——那个只在指针贴到连接点（connectionRadius 内）才有值，
 * 而这里要的是“线头压在节点身上”，所以拿 connection.pointer 自己和节点矩形比。
 *
 * 连线时指针每动一像素 store 都会更新一次，用 useConnection 订阅会把所有节点一起重渲染，
 * 因此直接订阅 store 并把结果写进 MotionValue：动画全程零 React 渲染。
 */
export function useConnectionTilt({ canAccept }: ConnectionTiltOptions = {}): ConnectionTilt {
  const nodeId = useNodeId();
  const store = useStoreApi();
  const prefersReducedMotion = useReducedMotion();

  // 订阅只建一次，判断函数却可能每次渲染都是新的，放进 ref 里现用现取
  const canAcceptRef = useRef(canAccept);
  useEffect(() => {
    canAcceptRef.current = canAccept;
  });

  const rotateX = useSpring(0, SPRING);
  const rotateY = useSpring(0, SPRING);
  const scale = useSpring(1, SPRING);
  const opacity = useSpring(1, SPRING);
  const transformPerspective = useTransform(() =>
    tiltPerspective(
      { rotateX: rotateX.get(), rotateY: rotateY.get(), scale: scale.get() },
      TILT_PERSPECTIVE,
    ),
  );

  useEffect(() => {
    const rest = () => {
      rotateX.set(0);
      rotateY.set(0);
      scale.set(1);
    };

    // 订阅撤掉时连同淡化一起还原，免得节点卡在半透明
    const reset = () => {
      rest();
      opacity.set(1);
    };

    if (!nodeId) return reset;

    const unsubscribe = store.subscribe((state, previous) => {
      // store 里什么都可能变，只有连线状态换了对象才值得往下算
      const { connection } = state;
      if (connection === previous.connection) return;

      // 没人在拉线（包括松手、落空、取消），或拉线的就是自己：倾斜和淡化都还原
      if (!connection.inProgress || connection.fromNode.id === nodeId) {
        reset();
        return;
      }

      const from = {
        nodeId: connection.fromNode.id,
        handleType: connection.fromHandle.type,
      };
      // 接不上的线不给倾斜反馈，免得沉下去了松手却连不上；节点整体淡下去
      const accepted = !canAcceptRef.current || canAcceptRef.current(from);
      opacity.set(accepted ? 1 : DIMMED_OPACITY);
      if (!accepted || prefersReducedMotion) {
        rest();
        return;
      }

      const node = state.nodeLookup.get(nodeId);
      if (!node) return;

      // pointer 是相对画布容器的屏幕坐标，换算回画布坐标才能和节点位置比
      const [translateX, translateY, zoom] = state.transform;
      const hit = getNodeHit(node, {
        x: (connection.pointer.x - translateX) / zoom,
        y: (connection.pointer.y - translateY) / zoom,
      });
      if (!hit) {
        rest();
        return;
      }

      // 让离鼠标近的那条边往里沉：rotateX 正值抬底边、负值把底边压下去，rotateY 正值推开右边
      rotateX.set(-hit.y * MAX_TILT);
      rotateY.set(hit.x * MAX_TILT);
      scale.set(HOVER_SCALE);
    });
    return () => {
      unsubscribe();
      reset();
    };
  }, [nodeId, opacity, prefersReducedMotion, rotateX, rotateY, scale, store]);

  return { rotateX, rotateY, scale, opacity, transformPerspective };
}
