/** 修饰键在提示里的写法：Mac 上是 ⌘，其余是 Ctrl */
export const MOD =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘" : "Ctrl+";

export function isCanvasShortcutBusy(event: KeyboardEvent) {
  return event.isComposing || isCanvasInputTarget(event.target);
}

export function isCanvasInputTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
    return true;
  return !!target.closest("[role=dialog], [role=menu], [role=listbox]");
}
