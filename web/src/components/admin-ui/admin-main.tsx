import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/** 后台页面主体（设计稿样式）：统一内边距，内容铺满宽度 */
function AdminMain({ className, ...props }: ComponentProps<"main">) {
  return <main data-slot="admin-main" className={cn("px-4 py-6 lg:px-6", className)} {...props} />;
}

export { AdminMain };
