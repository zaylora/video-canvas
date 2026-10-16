import type { AssetKind } from "@/api/asset-library/type";

/** 内容类型的筛选项：只有图片、视频、音频，没有文档 */
export const ASSET_KINDS: { value: AssetKind; label: string }[] = [
  { value: "image", label: "图片" },
  { value: "video", label: "视频" },
  { value: "audio", label: "音频" },
];

/**
 * 内容类型标签：生成历史和个人资产库共用。
 * @param value 当前类型
 * @param onChange 切换类型
 */
export function AssetKindTabs({
  value,
  onChange,
}: {
  value: AssetKind;
  onChange: (kind: AssetKind) => void;
}) {
  return (
    <div data-slot="asset-kind-tabs" className="flex gap-0.5" role="group" aria-label="内容类型">
      {ASSET_KINDS.map((item) => (
        <button
          key={item.value}
          type="button"
          aria-pressed={value === item.value}
          onClick={() => onChange(item.value)}
          className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 aria-pressed:text-foreground h-8 rounded-lg px-3 transition-colors outline-none focus-visible:ring-3 aria-pressed:font-semibold"
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
