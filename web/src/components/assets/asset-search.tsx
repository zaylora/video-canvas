import { Search } from "lucide-react";

/**
 * 资产页工具条里的搜索框（胶囊）：Esc 清空。
 * @param value 输入框的值
 * @param onChange 输入变化
 * @param placeholder 占位提示，同时作为读屏名字
 */
export function AssetSearch({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}) {
  return (
    <label
      data-slot="asset-search"
      className="bg-chrome-hover ring-chrome-border text-muted-foreground focus-within:ring-ring flex h-9 w-36 items-center gap-2 rounded-full px-3.5 ring-1 transition-shadow md:w-56"
    >
      <Search className="size-4 shrink-0" />
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => event.key === "Escape" && onChange("")}
        placeholder={placeholder}
        aria-label={placeholder}
        className="text-foreground min-w-0 flex-1 bg-transparent outline-none"
      />
    </label>
  );
}
