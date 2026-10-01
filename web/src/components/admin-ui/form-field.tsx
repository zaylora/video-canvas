import type { ReactNode } from "react";
import { CircleHelp } from "lucide-react";

import { Label } from "@/components/ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 表单字段：标题、控件、说明与错误。
 * size="sm"（默认）是紧凑的小标题，用在抽屉、对话框；size="default" 是设计稿编辑弹窗里的大标题，必填星号在前。
 */
function FormField({
  label,
  htmlFor,
  hint,
  error,
  required,
  tip,
  size = "sm",
  children,
  className,
}: {
  label: ReactNode;
  htmlFor?: string;
  hint?: ReactNode;
  error?: string;
  required?: boolean;
  /** 标题旁的 ? 提示 */
  tip?: string;
  size?: "sm" | "default";
  children: ReactNode;
  className?: string;
}) {
  const large = size === "default";
  return (
    <div
      data-slot="form-field"
      data-size={size}
      className={cn("flex min-w-0 flex-col", large ? "gap-2" : "gap-1.5", className)}
    >
      <Label
        htmlFor={htmlFor}
        className={large ? "gap-1 text-sm font-medium" : "text-muted-foreground text-xs"}
      >
        {large && required && <span className="text-red-500">*</span>}
        {label}
        {!large && required && <span className="text-destructive">*</span>}
        {tip && (
          <Tooltip>
            <TooltipTrigger render={<span className="inline-flex" aria-label={tip} />}>
              <CircleHelp className="text-muted-foreground size-3.5" />
            </TooltipTrigger>
            <TooltipContent className="max-w-60 leading-relaxed">{tip}</TooltipContent>
          </Tooltip>
        )}
      </Label>
      {children}
      {error ? (
        <p className="text-destructive text-xs" role="alert">
          {error}
        </p>
      ) : (
        hint && (
          <p
            className={cn(
              "text-muted-foreground",
              large ? "text-[0.8rem] leading-relaxed" : "text-xs",
            )}
          >
            {hint}
          </p>
        )
      )}
    </div>
  );
}

export { FormField };
