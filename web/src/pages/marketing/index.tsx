import { ShoppingBag } from "lucide-react";

import { MarketingComposer } from "@/components/marketing/marketing-composer";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useMarketingList } from "@/hooks/use-marketing-list";

import { ProjectGrid } from "./project-grid";

/** 列表页最多取几条 */
const PAGE_SIZE = 24;

/**
 * 营销工作室首页（/marketing）：上面是营销生成框（商品图 → 写脚本），下面是「我的营销片」卡片网格。
 * 卡片状态行和状态条随 WebSocket 推送刷新；没有项目时只有图标和一句标题。
 */
export default function MarketingListPage() {
  const { items, total, loading, failed, deletingId, reload, remove } = useMarketingList(PAGE_SIZE);

  return (
    <div className="mx-auto w-full max-w-320 px-4 pb-24 md:px-10">
      <section className="pt-4 md:pt-6">
        <h1 className="font-display text-center text-[26px] leading-tight font-black tracking-wide md:text-[34px]">
          把商品交给 <span className="text-beam">营销工作室</span>
        </h1>
        <div className="mx-auto mt-6 w-full max-w-270 md:mt-7">
          <MarketingComposer />
        </div>
      </section>

      <section className="mt-12 md:mt-14">
        <div className="mb-5 flex items-center gap-2">
          <h2 className="text-[17px] font-semibold">我的营销片</h2>
          {total > 0 && <span className="text-muted-foreground text-sm tabular-nums">{total}</span>}
        </div>

        {loading && items.length === 0 ? (
          <div className="grid grid-cols-2 gap-x-4 gap-y-7 sm:grid-cols-3 lg:grid-cols-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton
                key={i}
                className="aspect-16/10 rounded-[18px] motion-reduce:animate-none"
              />
            ))}
          </div>
        ) : failed && items.length === 0 ? (
          <div className="text-muted-foreground flex flex-col items-center gap-3 py-12 text-sm">
            营销片列表加载失败
            <Button variant="outline" size="sm" onClick={reload}>
              重试
            </Button>
          </div>
        ) : items.length === 0 ? (
          <div className="text-muted-foreground flex flex-col items-center gap-2 py-12 text-sm">
            <ShoppingBag className="size-8 opacity-50" />
            还没有营销片
          </div>
        ) : (
          <ProjectGrid items={items} deletingId={deletingId} onDelete={remove} />
        )}
      </section>
    </div>
  );
}
