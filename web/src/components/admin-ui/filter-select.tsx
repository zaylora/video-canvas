import { OptionSelect } from "@/components/admin-ui/option-select";
import { cn } from "@/lib/utils";

/**
 * 表格工具条的筛选下拉：第一个选项（值为空）是“全部”。选了别的值，描边加深，一眼看出哪些筛选在生效。
 * 选项用 options 传 [值, 文字]；其余属性同 OptionSelect。
 */
function FilterSelect({
  className,
  value,
  options,
  ...props
}: Omit<Parameters<typeof OptionSelect>[0], "options" | "size"> & {
  options: ReadonlyArray<readonly [value: string, label: string]>;
}) {
  return (
    <OptionSelect
      data-slot="filter-select"
      size="sm"
      value={value}
      options={options.map(([optionValue, label]) => ({ value: optionValue, label }))}
      className={cn("w-auto text-xs", value && "border-foreground/35", className)}
      {...props}
    />
  );
}

export { FilterSelect };
