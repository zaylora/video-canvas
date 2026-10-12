import type { ReactNode } from "react";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

/** 下拉里的一项；value 可以是空串（筛选里的“全部”） */
type SelectOption = {
  value: string;
  label: ReactNode;
  disabled?: boolean;
};

/** base-ui 的 Select 把 null 当“没选”，空串选项用哨兵值承接，对外仍是空串 */
const EMPTY = "__empty__";
const toInner = (value: string) => (value === "" ? EMPTY : value);
const fromInner = (value: string) => (value === EMPTY ? "" : value);

/**
 * 后台的下拉选择：shadcn 的 Select 加一个选项数组，取代原生 <select>。
 * 值始终是字符串；onValueChange 直接给选中的值（不是事件）。size="sm" 用在表格工具条，
 * id 落在触发按钮上，给 FormField 的 htmlFor 用；invalid 给校验不过时标红。
 */
function OptionSelect({
  value,
  onValueChange,
  options,
  placeholder,
  size = "default",
  id,
  disabled,
  invalid,
  className,
  contentClassName,
  "aria-label": ariaLabel,
}: {
  value: string;
  onValueChange: (value: string) => void;
  options: ReadonlyArray<SelectOption>;
  placeholder?: string;
  size?: "sm" | "default";
  id?: string;
  disabled?: boolean;
  invalid?: boolean;
  className?: string;
  contentClassName?: string;
  "aria-label"?: string;
}) {
  return (
    <Select
      value={toInner(value)}
      disabled={disabled}
      onValueChange={(next) => {
        if (typeof next === "string") onValueChange(fromInner(next));
      }}
    >
      <SelectTrigger
        data-slot="option-select"
        id={id}
        size={size}
        aria-label={ariaLabel}
        aria-invalid={invalid || undefined}
        className={cn("w-full", className)}
      >
        <SelectValue placeholder={placeholder}>
          {(inner: string) => options.find((o) => toInner(o.value) === inner)?.label ?? placeholder}
        </SelectValue>
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false} className={contentClassName}>
        {options.map((option) => (
          <SelectItem
            key={toInner(option.value)}
            value={toInner(option.value)}
            disabled={option.disabled}
          >
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export { OptionSelect };
export type { SelectOption };
