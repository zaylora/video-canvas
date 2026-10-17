import type { ProductDto } from "@/api/payment/type";
import { cn } from "@/lib/utils";
import { formatCredits, formatYuan } from "@/utils/payment/format";

interface ProductGridProps {
  /** 上架商品 */
  products: ProductDto[];
  /** 选中的商品 ID */
  selectedId: number | null;
  /** 选中变化；提交中等不可改选的阶段为 true，卡片整体禁用 */
  disabled: boolean;
  /** 点选某一档 */
  onSelect: (id: number) => void;
}

/** 商品网格：一行最多四档，每档显示到账积分、售价和赠送；选中的有描边，带角标的右上角显示角标 */
export function ProductGrid({ products, selectedId, disabled, onSelect }: ProductGridProps) {
  if (products.length === 0) {
    return (
      <div className="text-muted-foreground grid min-h-48 place-items-center rounded-xl border border-dashed text-sm">
        暂无可购买的积分商品
      </div>
    );
  }
  return (
    <div className="grid grid-cols-2 content-start gap-3 sm:grid-cols-3 lg:grid-cols-4">
      {products.map((p) => {
        const selected = p.id === selectedId;
        return (
          <button
            key={p.id}
            type="button"
            disabled={disabled}
            aria-pressed={selected}
            onClick={() => onSelect(p.id)}
            className={cn(
              "group bg-card overflow-hidden rounded-xl border text-left transition-[border-color,box-shadow] outline-none",
              "focus-visible:ring-ring/50 focus-visible:ring-2",
              selected ? "border-foreground ring-foreground ring-1" : "hover:border-foreground/40",
              disabled && "cursor-not-allowed opacity-60",
            )}
          >
            <div className="relative bg-linear-to-br from-neutral-900 to-neutral-700 p-3 text-white">
              <span className="text-[10px] tracking-[0.14em] opacity-70">✦ POWER CARD</span>
              {p.badge ? (
                <span className="bg-credit absolute top-2 right-2 rounded-full px-2 py-px text-[10.5px] font-semibold text-neutral-950">
                  {p.badge}
                </span>
              ) : null}
              <div className="mt-3 text-right font-mono text-xl font-semibold tabular-nums">
                {formatCredits(p.baseCredits + p.bonusCredits)}
                <span className="ml-1 text-[10px] font-normal opacity-70">PTS</span>
              </div>
            </div>
            <div className="px-3 py-2">
              <div className="flex items-center gap-1.5">
                <b className="text-[15px]">{formatYuan(p.priceFen)}</b>
                {p.bonusCredits > 0 ? (
                  <span className="bg-credit/15 text-credit rounded-full px-1.5 py-px text-[11px]">
                    +赠 {formatCredits(p.bonusCredits)}
                  </span>
                ) : null}
              </div>
              <div className="text-muted-foreground text-xs">
                到账 {formatCredits(p.baseCredits + p.bonusCredits)} 积分
              </div>
            </div>
          </button>
        );
      })}
    </div>
  );
}
