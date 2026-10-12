import type { Ref } from "react";
import { X } from "lucide-react";

import { SearchInput } from "@/components/admin-ui/search-input";
import { useDebouncedSearch } from "@/pages/admin/use-admin-list";
import { cn } from "@/lib/utils";

/**
 * 列表工具条里的搜索框：输入停 300ms 后才把搜索词交给 onCommit（发请求），右侧有清空叉。
 * URL 里的值变化（清除筛选、后退）时输入框跟着变。
 * @param value 已生效的搜索词（来自 URL）
 * @param onCommit 搜索词确定后
 * @param placeholder 占位提示，同时作为无障碍标签
 * @param ref 输入框的 ref（页面的快捷键聚焦用）
 */
function ListSearch({
  value,
  onCommit,
  placeholder,
  className,
  ref,
}: {
  value: string;
  onCommit: (value: string) => void;
  placeholder: string;
  className?: string;
  ref?: Ref<HTMLInputElement>;
}) {
  const [input, setInput] = useDebouncedSearch(value, onCommit);
  return (
    <SearchInput
      ref={ref}
      className={cn("w-full sm:w-64", className)}
      aria-label={placeholder}
      placeholder={placeholder}
      autoComplete="off"
      value={input}
      onChange={(event) => setInput(event.target.value)}
      trailing={
        input ? (
          <button
            type="button"
            aria-label="清空搜索"
            className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 inline-flex size-5 items-center justify-center rounded outline-none focus-visible:ring-2"
            onClick={() => {
              setInput("");
              onCommit("");
            }}
          >
            <X className="size-3.5" />
          </button>
        ) : undefined
      }
    />
  );
}

export { ListSearch };
