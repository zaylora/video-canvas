import {
  useEffect,
  useState,
  type FocusEvent,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { AnimatePresence } from "motion/react";

import { RefPeekCard } from "@/components/ref-peek";
import {
  PEEK_GAP,
  peekMedia,
  peekPlacement,
  peekSize,
  type PeekRect,
  type PeekSize,
  type PeekTarget,
} from "@/utils/canvas/ref-peek";

/** 挂在缩略图（或 chip）外框上的事件，用来触发悬停预览 */
export type PeekBind = {
  /** 鼠标移入：只认鼠标，触屏不触发 */
  onPointerEnter: (event: PointerEvent<HTMLElement>) => void;
  /** 鼠标移出 */
  onPointerLeave: (event: PointerEvent<HTMLElement>) => void;
  /** 键盘聚焦进来（:focus-visible）；鼠标点击留下的焦点不算 */
  onFocus: (event: FocusEvent<HTMLElement>) => void;
  /** 焦点离开外框 */
  onBlur: (event: FocusEvent<HTMLElement>) => void;
  /** Esc 收起预览，不让它顺手关掉外层对话框 */
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
};

/** 预览在视口里的层级：盖过对话框（z-50）和 @ 菜单（z-70） */
const PEEK_Z = "z-[80]";

/** 取元素此刻在视口里的矩形 */
const rectOf = (el: HTMLElement): PeekRect => {
  const { left, top, right, bottom, width, height } = el.getBoundingClientRect();
  return { left, top, right, bottom, width, height };
};

/**
 * 参考图的悬停预览（设计稿 10.1）：鼠标移上去或键盘聚焦就在缩略图正上方弹出大图，上方放不下翻到下方。
 * 预览 portal 到 body、固定定位，不被面板或对话框的 overflow 剪裁，也不受画布缩放影响；不拦鼠标。
 * 文本、音频、已断开、没有地址的引用不弹（见 canPeek）；悬停即显示，没有延迟。
 * 画布平移缩放、页面滚动或窗口变化时直接收起，免得预览留在原地。
 * @param target 要预览的引用
 * @param alt 图片的替代文字
 * @returns bind 展开到外框上；peek 渲染在外框旁边（任意位置，它自己 portal）
 */
export function useRefPeek(target: PeekTarget, alt?: string) {
  const media = peekMedia(target);
  const src = target.src ?? "";
  const [hover, setHover] = useState(false);
  const [focus, setFocus] = useState(false);
  const [anchor, setAnchor] = useState<PeekRect | null>(null);
  const [measured, setMeasured] = useState<{ src: string; size: PeekSize } | null>(null);
  const open = (hover || focus) && !!media && !!anchor;

  useEffect(() => {
    if (!open) return;
    const close = () => {
      setHover(false);
      setFocus(false);
    };
    // wheel 也要听：画布用滚轮平移缩放，不一定触发 scroll
    window.addEventListener("scroll", close, true);
    window.addEventListener("wheel", close, { capture: true, passive: true });
    window.addEventListener("resize", close);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("wheel", close, true);
      window.removeEventListener("resize", close);
    };
  }, [open]);

  const bind: PeekBind = {
    onPointerEnter: (event) => {
      if (event.pointerType !== "mouse" || !media) return;
      setAnchor(rectOf(event.currentTarget));
      setHover(true);
    },
    onPointerLeave: (event) => {
      if (event.pointerType === "mouse") setHover(false);
    },
    onFocus: (event) => {
      if (!media || !(event.target as Element).matches(":focus-visible")) return;
      setAnchor(rectOf(event.currentTarget));
      setFocus(true);
    },
    onBlur: (event) => {
      if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocus(false);
    },
    onKeyDown: (event) => {
      if (event.key !== "Escape" || !open) return;
      event.stopPropagation();
      setHover(false);
      setFocus(false);
    },
  };

  let card: ReactNode = null;
  if (open && media && anchor) {
    // 图还没加载出来时按最大框估；量出来之后按真实大小重新摆
    const size = peekSize(measured?.src === src ? measured.size : null);
    const viewport = { width: window.innerWidth, height: window.innerHeight };
    const place = peekPlacement({ anchor, size, viewport });
    card = (
      <RefPeekCard
        key={src}
        kind={media}
        src={src}
        alt={alt}
        side={place.side}
        originX={`${anchor.left + anchor.width / 2 - place.left}px`}
        className={`fixed ${PEEK_Z}`}
        // 上方的用 bottom 贴着目标，图的真实高度和估算不一致时也不会离开缩略图
        style={
          place.side === "above"
            ? { left: place.left, bottom: viewport.height - (anchor.top - PEEK_GAP) }
            : { left: place.left, top: place.top }
        }
        onNaturalSize={(natural) => setMeasured({ src, size: natural })}
      />
    );
  }
  const peek = createPortal(<AnimatePresence>{card}</AnimatePresence>, document.body);

  return { bind, peek };
}

/**
 * useRefPeek 的 render-prop 写法：给在列表里 map 出来的缩略图用（hook 不能直接放进 map）。
 * 子函数拿到 bind，展开到外框上；预览自己 portal，不占位置。
 * @param target 要预览的引用
 * @param alt 图片的替代文字
 * @param children 渲染缩略图外框的函数
 */
export function RefPeek({
  target,
  alt,
  children,
}: {
  target: PeekTarget;
  alt?: string;
  children: (bind: PeekBind) => ReactNode;
}) {
  const { bind, peek } = useRefPeek(target, alt);
  return (
    <>
      {children(bind)}
      {peek}
    </>
  );
}
