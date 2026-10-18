import type { ComponentProps } from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** 列表或抽屉请求失败：「加载失败 · 重试」（全局 toast 已弹，这里只给重试入口） */
function LoadFailed({
  onRetry,
  className,
  ...props
}: Omit<ComponentProps<"div">, "children"> & { onRetry: () => void }) {
  return (
    <div
      data-slot="load-failed"
      role="alert"
      className={cn(
        "text-muted-foreground flex items-center justify-center gap-2 py-12 text-sm",
        className,
      )}
      {...props}
    >
      加载失败 ·
      <Button variant="link" size="xs" className="px-0" onClick={onRetry}>
        重试
      </Button>
    </div>
  );
}

export { LoadFailed };
