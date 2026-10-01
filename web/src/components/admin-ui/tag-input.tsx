import { useState, type KeyboardEvent } from "react";
import { X } from "lucide-react";

import { Tag } from "@/components/admin-ui/tag";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * 标签输入：回车（或中文逗号 / 英文逗号）添加，点 × 或在空输入框里按退格删除。
 * 重复、空白、超长的标签直接忽略，数量到上限后输入框禁用。
 */
function TagInput({
  id,
  value,
  onChange,
  max = 5,
  maxLength = 12,
  placeholder = "输入后回车添加",
  className,
}: {
  id?: string;
  value: readonly string[];
  onChange: (next: string[]) => void;
  /** 最多几个标签 */
  max?: number;
  /** 单个标签最多几个字 */
  maxLength?: number;
  placeholder?: string;
  className?: string;
}) {
  const [draft, setDraft] = useState("");
  const full = value.length >= max;

  const commit = () => {
    const text = draft.trim();
    setDraft("");
    if (!text || text.length > maxLength || full || value.includes(text)) return;
    onChange([...value, text]);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing) return; // 中文输入法选词时的回车不算添加
    if (event.key === "Enter" || event.key === "," || event.key === "，") {
      event.preventDefault();
      commit();
    } else if (event.key === "Backspace" && !draft && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  };

  return (
    <div className={cn("space-y-2", className)}>
      <div className="flex flex-wrap gap-1.5 empty:hidden">
        {value.map((tag) => (
          <Tag key={tag} tone="info" className="gap-1 pr-1">
            {tag}
            <button
              type="button"
              aria-label={`删除标签 ${tag}`}
              className="hover:bg-foreground/10 rounded-sm"
              onClick={() => onChange(value.filter((item) => item !== tag))}
            >
              <X />
            </button>
          </Tag>
        ))}
      </div>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          value={draft}
          disabled={full}
          maxLength={maxLength}
          placeholder={full ? `最多 ${max} 个标签` : placeholder}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={onKeyDown}
          onBlur={commit}
        />
        <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
          {value.length}/{max}
        </span>
      </div>
    </div>
  );
}

export { TagInput };
