import { cn } from "@/lib/utils";
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
 * @param variant plain 选中加粗（资产页）；underline 选中带下划线（画布资产库弹框）
 */
export function AssetKindTabs({
  value,
  onChange,
  variant = "plain",
}: {
  value: AssetKind;
  onChange: (kind: AssetKind) => void;
  variant?: "plain" | "underline";
}) {
  return (
    <div data-slot="asset-kind-tabs" className="flex gap-0.5" role="group" aria-label="内容类型">
      {ASSET_KINDS.map((item) => (
        <button
          key={item.value}
          type="button"
          aria-pressed={value === item.value}
          onClick={() => onChange(item.value)}
          className={cn(
            "text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 aria-pressed:text-foreground h-8 rounded-lg px-3 transition-colors outline-none focus-visible:ring-3 aria-pressed:font-semibold",
            variant === "plain" && "hover:bg-chrome-hover",
            // 下划线只做 transform（scaleX），减少动态效果时不过渡
            variant === "underline" &&
              "after:bg-foreground relative rounded-md after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:origin-center after:scale-x-0 after:rounded-full after:transition-transform after:duration-150 motion-reduce:after:transition-none aria-pressed:after:scale-x-100",
          )}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
