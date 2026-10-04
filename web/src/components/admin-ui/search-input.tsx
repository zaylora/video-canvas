import type { ComponentProps, ReactNode } from "react";
import { Search } from "lucide-react";

import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * 左侧带放大镜图标的搜索框；className 作用在外层，宽度在这里控制。
 * trailing 是输入框右侧的内容（清空按钮、快捷键提示），传了就给输入留出右侧空间并隐藏浏览器自带的清空叉。
 */
function SearchInput({
  className,
  inputClassName,
  trailing,
  ...props
}: Omit<ComponentProps<typeof Input>, "type"> & {
  /** 作用在输入框本身的类名 */
  inputClassName?: string;
  /** 输入框右侧的内容 */
  trailing?: ReactNode;
}) {
  return (
    <div data-slot="search-input" className={cn("relative", className)}>
      <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
      <Input
        type="search"
        className={cn(
          "pl-8",
          trailing && "pr-14 [&::-webkit-search-cancel-button]:hidden",
          inputClassName,
        )}
        {...props}
      />
      {trailing && (
        <span className="absolute top-1/2 right-2 flex -translate-y-1/2 items-center gap-1">
          {trailing}
        </span>
      )}
    </div>
  );
}

export { SearchInput };
