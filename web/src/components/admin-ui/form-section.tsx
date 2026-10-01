import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 表单里的一块（设计稿 block）：标题 + 一句说明 + 内容。
 * <FormSection><FormSectionHeader><FormSectionTitle /><FormSectionDescription /></FormSectionHeader>…</FormSection>
 */
function FormSection({ className, ...props }: ComponentProps<"section">) {
  return (
    <section data-slot="form-section" className={cn("mb-7 last:mb-0", className)} {...props} />
  );
}

function FormSectionHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="form-section-header"
      className={cn("mb-3 flex flex-wrap items-baseline gap-x-2", className)}
      {...props}
    />
  );
}

function FormSectionTitle({ className, ...props }: ComponentProps<"h3">) {
  return (
    <h3
      data-slot="form-section-title"
      className={cn("text-sm font-semibold", className)}
      {...props}
    />
  );
}

function FormSectionDescription({ className, ...props }: ComponentProps<"p">) {
  return (
    <p
      data-slot="form-section-description"
      className={cn("text-muted-foreground text-xs", className)}
      {...props}
    />
  );
}

export { FormSection, FormSectionDescription, FormSectionHeader, FormSectionTitle };
