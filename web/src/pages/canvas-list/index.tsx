import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Settings2 } from "lucide-react";
import { createCanvas, getCanvasList } from "@/api/canvas";
import type { CanvasListItemDto } from "@/api/canvas/type";
import { Button, buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminStore } from "@/store/admin";
import { canManageModels } from "@/utils/admin/role";
import { rememberCanvasTitle } from "@/utils/canvas/title-cache";

export default function CanvasList() {
  const navigate = useNavigate();
  const [items, setItems] = useState<CanvasListItemDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  /**
   * 只有“已知是管理员”的会话才显示 AI 配置入口（store 里角色已确认）。
   * 这里不主动探测角色：普通用户调 /admin/ai/me 会得到 403，拦截器会弹全局 toast，
   * 而拦截器没有跳过开关；等后端在通用用户接口里带 role 后再改为登录后直接判断。
   */
  const isAdmin = useAdminStore((state) => state.status === "ready" && canManageModels(state.role));
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
        <div className="flex items-center gap-2">
          {isAdmin && (
            <Link to="/admin/ai" className={buttonVariants({ variant: "outline" })}>
              <Settings2 />
              AI 配置
            </Link>
          )}
          <Button disabled={creating} onClick={() => void create()}>
            {creating ? "正在创建…" : "新建画布"}
          </Button>
        </div>
      </header>
      {loading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }, (_, index) => (
            <div key={index} className="bg-card overflow-hidden rounded-xl border shadow-sm">
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
                  <img src={canvas.coverUrl} alt="" className="size-full object-cover" />
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
