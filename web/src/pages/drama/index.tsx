import { useCallback, useEffect, useState } from "react";
import { Film } from "lucide-react";
import { toast } from "sonner";

import { deleteDrama, listDramas } from "@/api/drama";
import type { DramaSummary } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

import { DramaGrid } from "./drama-grid";
import { UploadCard } from "./upload-card";

const PAGE_SIZE = 24;

/**
 * 短剧工作室首页（/drama）：上面是创作卡（上传 / 粘贴剧本或小说），下面是「我的短剧」。
 * 列表按更新时间倒序，分页「加载更多」；删除成功后就地从列表移除。
 */
export default function DramaListPage() {
  const [items, setItems] = useState<DramaSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const load = useCallback(async (next: number) => {
    setLoading(true);
    setFailed(false);
    try {
      const result = await listDramas({ page: next, pageSize: PAGE_SIZE });
      setItems((prev) => (next === 1 ? result.items : [...prev, ...result.items]));
      setTotal(result.total);
      setPage(next);
    } catch {
      setFailed(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(1);
  }, [load]);

  const remove = async (id: string) => {
    setDeletingId(id);
    try {
      await deleteDrama(id);
      setItems((prev) => prev.filter((item) => item.id !== id));
      setTotal((prev) => Math.max(0, prev - 1));
      toast.success("已删除");
    } catch {
      // 请求层已经弹了错误提示
    } finally {
      setDeletingId(null);
    }
  };

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
            <Button variant="outline" size="sm" onClick={() => void load(1)}>
              重试
            </Button>
          </div>
        ) : items.length === 0 ? (
          <div className="text-muted-foreground flex flex-col items-center gap-2 py-12 text-sm">
            <Film className="size-8 opacity-50" />
            还没有短剧，在上面上传一份剧本开始吧
          </div>
        ) : (
          <>
            <DramaGrid items={items} deletingId={deletingId} onDelete={(id) => void remove(id)} />
            {items.length < total && (
              <div className="mt-8 flex justify-center">
                <Button variant="outline" disabled={loading} onClick={() => void load(page + 1)}>
                  {loading ? "加载中…" : "加载更多"}
                </Button>
              </div>
            )}
          </>
        )}
      </section>
    </div>
  );
}
