import { Search } from "lucide-react";

/**
 * 资产页右上角的搜索框（和旧资产页同款）：Esc 清空。
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
      className="bg-muted text-muted-foreground focus-within:ring-ring flex h-8 w-33 items-center gap-1.5 rounded-[10px] px-2.5 transition-shadow focus-within:ring-1 md:w-50"
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
