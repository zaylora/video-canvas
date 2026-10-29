import { Link } from "react-router";
import { ChevronRight, Film } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useDramaList } from "@/hooks/use-drama-list";

import { DramaGrid } from "./drama-grid";
import { UploadCard } from "./upload-card";

/** 首页只放最近的几部，其余去作品页的「个人短剧」看 */
const PREVIEW_COUNT = 4;

/**
 * 短剧工作室首页（/drama）：上面是创作卡（上传 / 粘贴剧本或小说），下面是「我的短剧」最近 4 部；
 * 超过 4 部时标题旁有「更多」，进作品页的「个人短剧」（/canvases?scope=drama）看全部。
 */
export default function DramaListPage() {
  const { items, total, loading, failed, deletingId, reload, remove } = useDramaList(PREVIEW_COUNT);

  return (
    <div className="mx-auto w-full max-w-320 px-4 pb-24 md:px-10">
      <section className="pt-4 md:pt-6">
        <h1 className="font-display text-center text-[26px] leading-tight font-black tracking-wide md:text-[34px]">
          把剧本交给 <span className="text-beam">短剧工作室</span>
        </h1>
        <UploadCard />
      </section>

      <section className="mt-12 md:mt-14">
        <div className="mb-5 flex items-center gap-2">
          <h2 className="text-[17px] font-semibold">我的短剧</h2>
          {total > 0 && <span className="text-muted-foreground text-sm tabular-nums">{total}</span>}
          {total > PREVIEW_COUNT && (
            <Link
              to="/canvases?scope=drama"
              className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 ml-auto inline-flex h-8 items-center gap-0.5 rounded-full pr-2 pl-3 text-[13px] transition-colors outline-none focus-visible:ring-2"
            >
              更多
              <ChevronRight className="size-4" />
            </Link>
          )}
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
            短剧列表加载失败
            <Button variant="outline" size="sm" onClick={reload}>
              重试
            </Button>
          </div>
        ) : items.length === 0 ? (
          <div className="text-muted-foreground flex flex-col items-center gap-2 py-12 text-sm">
            <Film className="size-8 opacity-50" />
            还没有短剧，在上面上传一份剧本开始吧
          </div>
        ) : (
          <DramaGrid items={items} deletingId={deletingId} onDelete={remove} />
        )}
      </section>
    </div>
  );
}
