import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 空状态 / 占位：
 * <EmptyState><EmptyStateIcon><Construction /></EmptyStateIcon><EmptyStateTitle /><EmptyStateDescription /><EmptyStateActions /></EmptyState>
 */
function EmptyState({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-state"
      className={cn(
        "text-muted-foreground flex min-h-48 flex-col items-center justify-center gap-2 rounded-xl border border-dashed p-8 text-center",
        className,
      )}
      {...props}
    />
  );
}

function EmptyStateIcon({ className, ...props }: ComponentProps<"div">) {
  return (
    <div data-slot="empty-state-icon" className={cn("mb-1 [&>svg]:size-8", className)} {...props} />
  );
}

function EmptyStateTitle({ className, ...props }: ComponentProps<"p">) {
  return (
    <p
      data-slot="empty-state-title"
      className={cn("text-foreground text-sm font-medium", className)}
      {...props}
    />
  );
}

function EmptyStateDescription({ className, ...props }: ComponentProps<"p">) {
  return (
    <p
      data-slot="empty-state-description"
      className={cn("max-w-md text-sm", className)}
      {...props}
    />
  );
}

function EmptyStateActions({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-state-actions"
      className={cn("mt-2 flex items-center gap-2", className)}
      {...props}
    />
  );
}

export { EmptyState, EmptyStateActions, EmptyStateDescription, EmptyStateIcon, EmptyStateTitle };
