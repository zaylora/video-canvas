import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { createCanvas, getCanvasList } from "@/api/canvas";
import type { CanvasListItemDto } from "@/api/canvas/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { rememberCanvasTitle } from "@/utils/canvas/title-cache";

export default function CanvasList() {
  const navigate = useNavigate();
  const [items, setItems] = useState<CanvasListItemDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  useEffect(() => {
    let active = true;
    void getCanvasList()
      .then((result) => {
        if (!active) return;
        const list = Array.isArray(result?.items) ? result.items : [];
        // 记下画布名，任务在别处完成时的 toast 要用
        for (const item of list) rememberCanvasTitle(item.id, item.title);
        setItems(list);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  const create = async () => {
    if (creating) return;
    setCreating(true);
    try {
      const canvas = await createCanvas();
      navigate(`/canvas/${canvas.id}`);
    } finally {
      setCreating(false);
    }
  };
  return (
    <main className="mx-auto min-h-svh max-w-6xl p-8">
      <header className="mb-8 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">我的画布</h1>
        <Button disabled={creating} onClick={() => void create()}>
          {creating ? "正在创建…" : "新建画布"}
        </Button>
      </header>
      {loading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }, (_, index) => (
            <div
              key={index}
              className="bg-card overflow-hidden rounded-xl border shadow-sm"
            >
              <Skeleton className="aspect-video rounded-none" />
              <div className="p-4">
                <Skeleton className="h-5 w-1/2" />
                <Skeleton className="mt-2 h-4 w-1/3" />
              </div>
            </div>
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="text-muted-foreground">还没有画布，先建一张吧。</p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((canvas) => (
            <button
              key={canvas.id}
              className="bg-card overflow-hidden rounded-xl border text-left shadow-sm"
              onClick={() => navigate(`/canvas/${canvas.id}`)}
            >
              <div className="bg-muted aspect-video">
                {canvas.coverUrl && (
                  <img
                    src={canvas.coverUrl}
                    alt=""
                    className="size-full object-cover"
                  />
                )}
              </div>
              <div className="p-4">
                <div className="font-medium">{canvas.title}</div>
                <div className="text-muted-foreground mt-1 text-xs">
                  {new Date(canvas.updatedAt).toLocaleString()}
                </div>
              </div>
            </button>
          ))}
        </div>
      )}
    </main>
  );
}
