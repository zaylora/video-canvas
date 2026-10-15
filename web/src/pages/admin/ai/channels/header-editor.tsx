import { Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { MAX_CHANNEL_HEADERS, type HeaderRow } from "@/utils/admin/channel-form";

/** “添加 User-Agent”快捷按钮填入的示例值，提示用户改成自己的 */
const DEFAULT_UA_VALUE = "MyApp/1.0";

/**
 * 自定义请求头编辑器：名称 / 值逐行，最多 32 条，附“添加 User-Agent”快捷按钮；
 * 每行的错误（来自 checkHeaders）显示在该行下面，条数超限的整体错误显示在底部。
 * 说明文字写明：默认发宿主 UA、添加后覆盖、仅发往本渠道 Base URL、值明文存储、禁用头不能设置。
 * @param rows 当前行
 * @param errors 校验错误：`headers.<行号>` 与 `headers`
 * @param disabled 禁用（保存中）
 * @param onChange 行变化
 */
export function HeaderEditor({
  rows,
  errors,
  disabled,
  onChange,
}: {
  rows: HeaderRow[];
  errors: Record<string, string>;
  disabled?: boolean;
  onChange: (rows: HeaderRow[]) => void;
}) {
  const hasUa = rows.some((row) => row.name.trim().toLowerCase() === "user-agent");
  const full = rows.length >= MAX_CHANNEL_HEADERS;
  const patch = (index: number, next: Partial<HeaderRow>) =>
    onChange(rows.map((row, i) => (i === index ? { ...row, ...next } : row)));

  return (
    <div className="flex flex-col gap-2 rounded-lg border p-3">
      <div className="text-sm font-medium">自定义请求头</div>

      {rows.length > 0 && (
        <ul className="flex flex-col gap-2">
          {rows.map((row, index) => {
            const error = errors[`headers.${index}`];
            return (
              <li key={index} className="flex flex-col gap-1">
                <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto] items-center gap-2">
                  <Input
                    aria-label={`请求头 ${index + 1} 名称`}
                    placeholder="名称"
                    spellCheck={false}
                    autoComplete="off"
                    value={row.name}
                    disabled={disabled}
                    aria-invalid={!!error}
                    onChange={(event) => patch(index, { name: event.target.value })}
                  />
                  <Input
                    aria-label={`请求头 ${index + 1} 值`}
                    placeholder="值"
                    spellCheck={false}
                    autoComplete="off"
                    value={row.value}
                    disabled={disabled}
                    aria-invalid={!!error}
                    onChange={(event) => patch(index, { value: event.target.value })}
                  />
                  <Button
                    type="button"
                    size="icon-sm"
                    variant="ghost"
                    aria-label={`删除请求头 ${index + 1}`}
                    disabled={disabled}
                    onClick={() => onChange(rows.filter((_, i) => i !== index))}
                  >
                    <Trash2 />
                  </Button>
                </div>
                {error && (
                  <p className="text-destructive text-xs" role="alert">
                    {error}
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          size="xs"
          variant="outline"
          disabled={disabled || full || hasUa}
          onClick={() => onChange([...rows, { name: "User-Agent", value: DEFAULT_UA_VALUE }])}
        >
          添加 User-Agent
        </Button>
        <Button
          type="button"
          size="xs"
          variant="outline"
          disabled={disabled || full}
          onClick={() => onChange([...rows, { name: "", value: "" }])}
        >
          <Plus />
          添加请求头
        </Button>
        <span className="text-muted-foreground ml-auto text-xs tabular-nums">
          {rows.length}/{MAX_CHANNEL_HEADERS}
        </span>
      </div>
      {errors.headers && (
        <p className="text-destructive text-xs" role="alert">
          {errors.headers}
        </p>
      )}
    </div>
  );
}
