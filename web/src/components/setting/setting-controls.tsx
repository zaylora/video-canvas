import type { ReactNode } from "react";
import { cn } from "cn";

import { Button } from "@/components/ui/button";

/** 设置项的一行：左边文案，右边控件 */
export function SettingRow({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-4 py-1">
      <div className="min-w-0 space-y-1">
        <div className="text-sm font-medium">{title}</div>
        {hint && <div className="text-muted-foreground text-xs leading-5">{hint}</div>}
      </div>
      {children}
    </div>
  );
}

/** 一排互斥的小按钮，比单选框省地方，适合一行一项的布局 */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div className="bg-muted flex shrink-0 gap-1 rounded-lg p-1">
      {options.map((option) => {
        const active = option.value === value;

        return (
          <Button
            key={option.value}
            size="sm"
            variant="ghost"
            aria-pressed={active}
            className={cn(
              "text-muted-foreground",
              active && "bg-background text-foreground shadow-sm"
            )}
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </Button>
        );
      })}
    </div>
  );
}
