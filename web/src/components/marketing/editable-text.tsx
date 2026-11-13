import { useEffect, useRef, useState } from "react";

import { cn } from "@/lib/utils";

/**
 * 失焦即保存的文本框（单行按回车也保存，Esc 放弃）：
 * 本地先改着，内容和外部值不同才调用 onCommit；外部值变了（别处改了、保存后重扫）且没在输入时跟着更新。
 * 营销片的旁白、拆解的动作和台词都用它：改一处保存一处，不需要「保存」按钮。
 * @param value 外部（服务端）的值
 * @param onCommit 提交修改后的值（已去掉首尾空白）
 * @param multiline 多行（动作）；否则单行（旁白、台词）
 */
export function EditableText({
  value,
  onCommit,
  multiline = false,
  disabled,
  invalid,
  placeholder,
  ariaLabel,
  rows = 2,
  className,
}: {
  value: string;
  onCommit: (value: string) => void;
  multiline?: boolean;
  disabled?: boolean;
  /** 描红：问题旁白 */
  invalid?: boolean;
  placeholder?: string;
  ariaLabel: string;
  rows?: number;
  className?: string;
}) {
  const [draft, setDraft] = useState(value);
  const [focused, setFocused] = useState(false);
  /** Esc 放弃：随后的失焦不保存 */
  const cancelled = useRef(false);

  useEffect(() => {
    if (!focused) setDraft(value);
  }, [value, focused]);

  const handleFocus = () => {
    cancelled.current = false;
    setFocused(true);
  };

  const handleBlur = () => {
    setFocused(false);
    if (cancelled.current) {
      setDraft(value);
      return;
    }
    const next = draft.trim();
    if (next === value.trim()) {
      setDraft(value);
      return;
    }
    onCommit(next);
  };

  const classes = cn(
    "hover:bg-chrome-hover focus-visible:bg-chrome-hover w-full min-w-0 rounded-lg bg-transparent px-2 py-1 text-[13px] leading-relaxed outline-none transition-colors duration-120 disabled:opacity-60",
    invalid && "text-destructive ring-destructive/60 ring-1",
    multiline && "resize-none",
    className,
  );

  if (multiline) {
    return (
      <textarea
        value={draft}
        rows={rows}
        disabled={disabled}
        placeholder={placeholder}
        aria-label={ariaLabel}
        aria-invalid={invalid || undefined}
        onChange={(event) => setDraft(event.target.value)}
        onFocus={handleFocus}
        onBlur={handleBlur}
        className={classes}
      />
    );
  }
  return (
    <input
      type="text"
      value={draft}
      disabled={disabled}
      placeholder={placeholder}
      aria-label={ariaLabel}
      aria-invalid={invalid || undefined}
      onChange={(event) => setDraft(event.target.value)}
      onFocus={handleFocus}
      onBlur={handleBlur}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !event.nativeEvent.isComposing) event.currentTarget.blur();
        if (event.key === "Escape") {
          cancelled.current = true;
          event.currentTarget.blur();
        }
      }}
      className={classes}
    />
  );
}
