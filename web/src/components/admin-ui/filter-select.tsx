import type { ComponentProps } from "react";

import { NativeSelect } from "@/components/admin-ui/native-select";
import { cn } from "@/lib/utils";

/**
 * 表格工具条的筛选下拉：第一个选项（值为空）是“全部”。选了别的值，描边加深，一眼看出哪些筛选在生效。
 * 选项用 options 传 [值, 文字]；其余属性同 NativeSelect。
 */
function FilterSelect({
  className,
  value,
  options,
  ...props
}: Omit<ComponentProps<"select">, "children"> & {
  value: string;
  options: ReadonlyArray<readonly [value: string, label: string]>;
}) {
  return (
    <NativeSelect
      data-slot="filter-select"
      value={value}
      className={cn("h-8 w-auto text-xs", value && "border-foreground/35", className)}
      {...props}
    >
      {options.map(([optionValue, label]) => (
        <option key={optionValue} value={optionValue}>
          {label}
        </option>
      ))}
    </NativeSelect>
  );
}

export { FilterSelect };
