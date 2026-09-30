import { useState, type ReactNode } from "react";

import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { parseJsonText, toJsonText } from "@/utils/admin/json";

/**
 * 表单里 params / input_schema 的 JSON 编辑框：只写入合法的 JSON。
 * 编辑中的文本先放本地 state，解析成功才通过 onValid 写回正文（键顺序由 JSON.parse 保留）；
 * 解析失败时正文保持上一个合法值，框下提示行列。
 * 外部整体替换内容（切换模型、插入模板）时，调用方用 key 让它重新挂载。
 * @param value 正文里这个字段当前的值
 * @param onValid 解析成功后的新值
 */
export function JsonFieldEditor({
  id,
  label,
  hint,
  value,
  issue,
  rows = 8,
  actions,
  onValid,
}: {
  id: string;
  label: string;
  hint?: ReactNode;
  value: unknown;
  /** 后端校验对这个字段的问题（就地显示） */
  issue?: string;
  rows?: number;
  /** 标题右侧的操作按钮 */
  actions?: ReactNode;
  onValid: (value: unknown) => void;
}) {
  const [text, setText] = useState(() => toJsonText(value ?? {}));
  const parsed = parseJsonText(text);
  const error = parsed.ok
    ? null
    : `JSON 语法错误${parsed.line ? `（第 ${parsed.line} 行 第 ${parsed.column} 列）` : ""}：${parsed.message}`;

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <label htmlFor={id} className="text-muted-foreground text-xs">
          {label}
        </label>
        {actions && <div className="ml-auto flex items-center gap-1">{actions}</div>}
      </div>
      <Textarea
        id={id}
        value={text}
        rows={rows}
        spellCheck={false}
        wrap="off"
        aria-invalid={!!error || !!issue}
        className={cn(
          "resize-y font-mono text-xs leading-5",
          (error || issue) && "border-destructive",
        )}
        onChange={(event) => {
          const next = event.target.value;
          setText(next);
          const result = parseJsonText(next);
          if (result.ok) onValid(result.value);
        }}
      />
      {error ? (
        <p className="text-destructive text-xs" role="alert">
          {error}（修复前不会写入正文）
        </p>
      ) : issue ? (
        <p className="text-destructive text-xs">{issue}</p>
      ) : (
        hint && <p className="text-muted-foreground text-xs">{hint}</p>
      )}
    </div>
  );
}
