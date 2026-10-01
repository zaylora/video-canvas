import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";

import { Button } from "@/components/ui/button";

/**
 * 复制按钮：点击复制文本，短暂显示“已复制”。剪贴板不可用（非 https、被拒绝）时静默不复制，
 * 按钮文案不变，用户可以自己选中文字。这里不弹 toast：复制不是请求。
 */
function CopyButton({
  text,
  label = "复制",
  className,
  iconOnly,
}: {
  text: string;
  label?: string;
  className?: string;
  /** 只显示图标（用 aria-label 与 title 说明） */
  iconOnly?: boolean;
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
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      return;
    }
    setCopied(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1500);
  };
  return (
    <Button
      type="button"
      variant="ghost"
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
