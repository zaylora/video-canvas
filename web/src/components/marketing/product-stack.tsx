import { useRef } from "react";
import { ImagePlus, Loader2, X } from "lucide-react";

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";

/** 一张商品图：上传中 / 已上传 / 失败 */
export interface StackItem {
  id: string;
  /** 预览或素材地址；还没生成本地预览时为空 */
  url?: string;
  status: "uploading" | "done" | "error";
}

/**
 * 商品图一摞：收起时叠放、右下角写「张数/上限」；点开浮层平铺每一张，单张可移除，没满时末尾有加图格。
 * 首页生成框和商品信息弹窗共用。
 * @param items 已有商品图（第一张显示在最上面）
 * @param max 最多几张
 * @param min 至少留几张：到了下限单张不能再移除
 * @param size 收起时块的尺寸 class
 * @param disabled 不能改（只能看）
 * @param onAdd 选了新文件（可多选）
 * @param onRemove 移除一张
 */
export function ProductStack({
  items,
  max,
  min = 0,
  size = "h-20 w-16",
  disabled,
  onAdd,
  onRemove,
}: {
  items: StackItem[];
  max: number;
  min?: number;
  size?: string;
  disabled?: boolean;
  onAdd: (files: File[]) => void;
  onRemove: (id: string) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const top = items[0];
  const uploading = items.some((item) => item.status === "uploading");
  const failed = items.some((item) => item.status === "error");
  const canRemove = items.length > min && !disabled;

  return (
    <Popover>
      <input
        ref={input}
        type="file"
        accept="image/*"
        multiple
        hidden
        onChange={(event) => {
          const files = Array.from(event.currentTarget.files ?? []);
          event.currentTarget.value = "";
          if (files.length > 0) onAdd(files.slice(0, max - items.length));
        }}
      />
      <PopoverTrigger
        aria-label={`商品图 ${items.length}/${max}`}
        title={`商品图 ${items.length}/${max}`}
        className={cn(
          "group/stack focus-visible:ring-ring/50 relative block shrink-0 rounded-xl outline-none focus-visible:ring-3",
          size,
        )}
      >
        {items.length > 1 && (
          <span className="bg-chrome-hover ring-chrome-border absolute inset-0 translate-x-1 -translate-y-1 rotate-6 rounded-xl ring-1" />
        )}
        <span
          className={cn(
            "bg-muted ring-chrome-border absolute inset-0 overflow-hidden rounded-xl ring-1 transition-transform duration-150 group-hover/stack:-translate-y-0.5",
            failed && "ring-destructive ring-2",
          )}
        >
          {top?.url && <img src={top.url} alt="" className="size-full object-cover" />}
          <span className="from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 flex items-end justify-between bg-linear-to-t to-transparent px-1 pt-3 pb-1 text-[11px] leading-tight">
            {failed ? "上传失败" : "商品"}
            <span className="tabular-nums opacity-85">
              {items.length}/{max}
            </span>
          </span>
          {uploading && (
            <span className="bg-cover-scrim/60 text-cover-foreground absolute inset-0 grid place-items-center">
              <Loader2 className="size-4.5 animate-spin" />
            </span>
          )}
        </span>
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={8} className="w-auto gap-2.5 p-3">
        <span className="text-muted-foreground text-xs tabular-nums">
          商品图 {items.length}/{max}
        </span>
        <div className="flex gap-2">
          {items.map((item, index) => (
            <div key={item.id} className="group/item relative h-20 w-16 shrink-0">
              <div
                className={cn(
                  "bg-muted ring-chrome-border absolute inset-0 overflow-hidden rounded-xl ring-1",
                  item.status === "error" && "ring-destructive ring-2",
                )}
              >
                {item.url && <img src={item.url} alt="" className="size-full object-cover" />}
                {index === 0 && (
                  <span className="bg-cover-scrim text-cover-foreground absolute bottom-1 left-1 rounded-full px-1 text-[9px] leading-3.5">
                    主图
                  </span>
                )}
                {item.status === "uploading" && (
                  <span className="bg-cover-scrim/60 text-cover-foreground absolute inset-0 grid place-items-center">
                    <Loader2 className="size-4 animate-spin" />
                  </span>
                )}
                {item.status === "error" && (
                  <span className="bg-cover-scrim/60 text-cover-foreground absolute inset-0 grid place-items-center text-[11px]">
                    上传失败
                  </span>
                )}
              </div>
              {canRemove && (
                <button
                  type="button"
                  onClick={() => onRemove(item.id)}
                  aria-label={`移除第 ${index + 1} 张商品图`}
                  className="bg-cover-scrim text-cover-foreground focus-visible:ring-ring/50 absolute -top-1.5 -right-1.5 grid size-5 place-items-center rounded-full outline-none focus-visible:ring-2"
                >
                  <X className="size-3" />
                </button>
              )}
            </div>
          ))}
          {items.length < max && !disabled && (
            <button
              type="button"
              onClick={() => input.current?.click()}
              aria-label="加商品图"
              title="加商品图"
              className="bg-chrome-hover text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 grid h-20 w-16 shrink-0 place-items-center rounded-xl border border-dashed border-[color-mix(in_oklch,var(--foreground)_18%,transparent)] outline-none transition-colors focus-visible:ring-2"
            >
              <ImagePlus className="size-5" />
            </button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
