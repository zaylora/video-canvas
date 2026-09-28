import { useCallback, useEffect, useRef, useState } from "react";

/** 画布光标工具：箭头用于选中/框选，抓手用于拖动画布 */
export type CanvasTool = "select" | "pan";

/** 焦点落在能打字的地方时，空格是内容，不是快捷键 */
function isTypingTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;

  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

/**
 * 管理画布的光标工具。
 *
 * - `tool` 是控制条上点选的长期工具；
 * - 按住空格临时切到抓手，松开还原，得到的即时结果是 `activeTool`；
 * - 空格在拖动过程中松开时，延到鼠标抬起再还原，避免拖到一半被打断；
 * - 焦点在输入框里时空格照常打字，抓手让位给正在写的提示词。
 */
export function useCanvasTool() {
  const [tool, setTool] = useState<CanvasTool>("select");
  const [spaceHeld, setSpaceHeld] = useState(false);
  const pointerDownRef = useRef(false);
  const releaseOnPointerUpRef = useRef(false);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.code !== "Space") return;

      // 正在输入框里打字、或输入法正在组字，这一下空格是要写进去的内容
      if (event.isComposing || isTypingTarget(event.target)) return;

      // 长按会连发 keydown，每一下都得拦：漏掉重复的那些，空格就会落回页面
      // （滚动、重复点按当前焦点上的按钮）
      event.preventDefault();
      if (event.repeat) return;

      releaseOnPointerUpRef.current = false;
      setSpaceHeld(true);
    };

    const onKeyUp = (event: KeyboardEvent) => {
      if (event.code !== "Space") return;

      if (pointerDownRef.current) {
        releaseOnPointerUpRef.current = true;
        return;
      }
      setSpaceHeld(false);
    };

    const onPointerDown = () => {
      pointerDownRef.current = true;
    };

    const onPointerUp = () => {
      pointerDownRef.current = false;
      if (!releaseOnPointerUpRef.current) return;

      releaseOnPointerUpRef.current = false;
      setSpaceHeld(false);
    };

    // 切走窗口时收不到 keyup，回来时会卡在抓手，这里兜底还原
    const onBlur = () => {
      pointerDownRef.current = false;
      releaseOnPointerUpRef.current = false;
      setSpaceHeld(false);
    };

    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("blur", onBlur);

    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("blur", onBlur);
    };
  }, []);

  const toggleTool = useCallback(() => {
    setTool((current) => (current === "select" ? "pan" : "select"));
  }, []);

  /** 叠加空格临时态后当前真正生效的工具 */
  const activeTool: CanvasTool = spaceHeld ? "pan" : tool;

  return { tool, activeTool, toggleTool };
}
