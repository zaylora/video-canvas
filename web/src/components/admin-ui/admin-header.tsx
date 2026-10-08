import type { ComponentProps } from "react";

import { SidebarTrigger } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";

/**
 * 后台顶栏：吸顶、半透明毛玻璃、不画分隔线（和原型一样轻），侧栏开关 + children（面包屑、角色、主题）。
 */
function AdminHeader({ className, children, ...props }: ComponentProps<"header">) {
  return (
    <header
      data-slot="admin-header"
      className={cn(
        "bg-background/85 sticky top-0 z-20 flex h-14 shrink-0 items-center gap-3 px-4 backdrop-blur lg:px-6",
        className,
      )}
      {...props}
    >
      <SidebarTrigger className="size-8" />
      {children}
    </header>
  );
}

export { AdminHeader };
