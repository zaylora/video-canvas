/**
 * 复制文本到剪贴板的两条路：先用异步剪贴板 API；它不存在（页面不是 https，navigator.clipboard 是 undefined）
 * 或被拒绝时，退回“隐藏文本框 + execCommand('copy')”。两条都不行返回 false，由调用方决定怎么提示用户。
 * 读写依赖都可以注入，方便在没有 DOM 的测试里验证每条分支。
 */
export type ClipboardDeps = {
  /** 异步剪贴板写入；不可用时为 undefined */
  write?: (text: string) => Promise<void>;
  /** 兜底复制，返回是否成功；不可用时为 undefined */
  fallback?: (text: string) => boolean;
};

/** 浏览器里的默认依赖 */
function browserDeps(): ClipboardDeps {
  const clipboard = typeof navigator === "undefined" ? undefined : navigator.clipboard;
  return {
    write: clipboard ? (text) => clipboard.writeText(text) : undefined,
    fallback: typeof document === "undefined" ? undefined : copyByTextarea,
  };
}

/** 隐藏文本框 + execCommand('copy')：非 https 页面里剪贴板 API 不可用时的老办法 */
function copyByTextarea(text: string): boolean {
  const box = document.createElement("textarea");
  box.value = text;
  box.setAttribute("readonly", "");
  box.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
  const active = document.activeElement as HTMLElement | null;
  document.body.appendChild(box);
  try {
    box.select();
    return document.execCommand("copy");
  } finally {
    document.body.removeChild(box);
    active?.focus?.();
  }
}

/**
 * 复制文本。
 * @param text 要复制的文本
 * @param deps 读写依赖，默认用浏览器的；测试里传假的
 * @returns 是否复制成功；从不抛错
 */
export async function copyToClipboard(
  text: string,
  deps: ClipboardDeps = browserDeps(),
): Promise<boolean> {
  if (deps.write) {
    try {
      await deps.write(text);
      return true;
    } catch {
      // 被拒绝（权限、页面没有焦点）：往下走兜底
    }
  }
  if (!deps.fallback) return false;
  try {
    return deps.fallback(text);
  } catch {
    return false;
  }
}
