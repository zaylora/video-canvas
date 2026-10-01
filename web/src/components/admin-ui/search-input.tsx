import type { ComponentProps } from "react";
import { Search } from "lucide-react";

import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/** 左侧带放大镜图标的搜索框；className 作用在外层，宽度在这里控制 */
function SearchInput({ className, ...props }: Omit<ComponentProps<typeof Input>, "type">) {
  return (
    <div data-slot="search-input" className={cn("relative", className)}>
      <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
      <Input type="search" className="pl-8" {...props} />
    </div>
  );
}

export { SearchInput };
