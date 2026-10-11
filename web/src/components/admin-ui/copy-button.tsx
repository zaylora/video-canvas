import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";

import { Button } from "@/components/ui/button";
import { copyToClipboard } from "@/utils/admin/clipboard";

/**
 * 复制按钮：点击复制文本，短暂显示“已复制”。先用异步剪贴板 API，不可用（页面不是 https、被拒绝）时
 * 退回隐藏文本框的老办法（见 copyToClipboard）；两条路都不行时默认静默，按钮文案不变，用户可以自己选中文字。
 * 复制的文本不在界面上展示、用户没有退路时（如“复制修复信息”），传 onError 给出提示。
 */
function CopyButton({
  text,
  label = "复制",
  className,
  iconOnly,
  variant = "ghost",
  onError,
}: {
  text: string;
  label?: string;
  className?: string;
  /** 只显示图标（用 aria-label 与 title 说明） */
  iconOnly?: boolean;
  /** 按钮样式：默认 ghost（贴着内容的小操作）；放在提示条里当主操作时用 outline，和旁边的按钮一致 */
  variant?: "ghost" | "outline";
  /** 两种复制方式都失败时调用；不传则静默 */
  onError?: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  const copy = async () => {
    if (!(await copyToClipboard(text))) {
      onError?.();
      return;
    }
    setCopied(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1500);
  };
  return (
    <Button
      type="button"
      variant={variant}
      size={iconOnly ? "icon-xs" : "xs"}
      className={className}
      aria-label={iconOnly ? label : undefined}
      title={iconOnly ? label : undefined}
      onClick={() => void copy()}
    >
      {copied ? <Check /> : <Copy />}
      {!iconOnly && (copied ? "已复制" : label)}
    </Button>
  );
}

export { CopyButton };
