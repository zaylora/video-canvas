import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 页面标题区：
 * <PageHeader>
 *   <PageHeaderHeading><PageHeaderTitle /></PageHeaderHeading>
 *   <PageHeaderActions />
 * </PageHeader>
 */
function PageHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="page-header"
      className={cn("mb-5 flex flex-wrap items-end justify-between gap-3", className)}
      {...props}
    />
  );
}

function PageHeaderHeading({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="page-header-heading" className={cn("min-w-0", className)} {...props} />;
}

function PageHeaderTitle({ className, ...props }: ComponentProps<"h1">) {
  return (
    <h1
      data-slot="page-header-title"
      className={cn("text-2xl font-semibold tracking-tight", className)}
      {...props}
    />
  );
}

function PageHeaderActions({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="page-header-actions"
      className={cn("flex flex-wrap items-center gap-2", className)}
      {...props}
    />
  );
}

export { PageHeader, PageHeaderActions, PageHeaderHeading, PageHeaderTitle };
