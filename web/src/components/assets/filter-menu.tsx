import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

/** 筛选下拉的一个选项 */
export type FilterOption = {
  /** 选项值，单选组内唯一 */
  value: string;
  /** 显示文案 */
  label: string;
};

/** 一组选项：带标题的组前面会画一条分隔线 */
export type FilterGroup = {
  /** 组标题，可省略 */
  heading?: string;
  /** 选项 */
  options: FilterOption[];
};

/**
 * 资产页工具条里的筛选下拉（来源 / 时间 / 排序共用）：单选；偏离默认值时按钮文案换成当前选项并加深，
 * 让人一眼看出筛选在生效。
 * @param label 默认文案，也是读屏名字，如「来源」
 * @param value 当前选中的值
 * @param defaultValue 默认值：当前值等于它时按钮不高亮
 * @param groups 选项分组
 * @param onChange 选择
 * @param onOpenChange 打开或收起（来源下拉在第一次打开时才去拉画布列表）
 * @param footer 选项后面的说明（加载中、没有画布）
 */
export function FilterMenu({
  label,
  value,
  defaultValue,
  groups,
  onChange,
  onOpenChange,
  footer,
}: {
  label: string;
  value: string;
  defaultValue: string;
  groups: FilterGroup[];
  onChange: (value: string) => void;
  onOpenChange?: (open: boolean) => void;
  footer?: ReactNode;
}) {
  const active = value !== defaultValue;
  const current = groups.flatMap((group) => group.options).find((option) => option.value === value);

  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger
        aria-label={label}
        className={cn(
          "text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 inline-flex h-8 max-w-40 items-center gap-1 rounded-lg px-3 transition-colors outline-none focus-visible:ring-3",
          "data-popup-open:bg-chrome-hover",
          active && "text-foreground font-medium",
        )}
      >
        <span className="truncate">{active && current ? current.label : label}</span>
        <ChevronDown className="size-3.5 shrink-0" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={6} className="max-h-80 w-44 overflow-y-auto">
        <DropdownMenuRadioGroup value={value} onValueChange={(next) => onChange(String(next))}>
          {groups.map((group, index) => (
            <DropdownMenuGroup key={group.heading ?? index}>
              {index > 0 && <DropdownMenuSeparator />}
              {group.heading && <DropdownMenuLabel>{group.heading}</DropdownMenuLabel>}
              {group.options.map((option) => (
                <DropdownMenuRadioItem key={option.value} value={option.value}>
                  <span className="truncate">{option.label}</span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuGroup>
          ))}
        </DropdownMenuRadioGroup>
        {footer}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
