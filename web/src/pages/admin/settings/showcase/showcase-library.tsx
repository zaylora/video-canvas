import { useCallback, useEffect, useState } from "react";
import { Check, Loader2, TriangleAlert, X } from "lucide-react";

import { getShowcaseLibrary } from "@/api/admin/showcase";
import type { ShowcaseLibraryItem } from "@/api/admin/showcase/type";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/admin-ui/dialog";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { formatCanvasTime } from "@/utils/home/home";
import { showcaseWarnings } from "@/utils/showcase/rules";

import { useAliveRef, type LoadStatus } from "../../use-admin";

/** 素材库一页取多少条 */
const PAGE_SIZE = 48;

/**
 * 从素材库添加（设计稿：居中对话框、卡片网格、多选）：只列出平台生成的视频，
 * 提示词和模型从生成记录自动带入；已经在片单里的置灰并标「已在片单」。
 * 缩略图用视频首帧（素材库里的生成视频没有单独的封面）。
 * @param open 是否打开
 * @param onAdd 点「添加」，带上选中的视频；调用方负责创建，抛错时对话框保持打开
 */
export function ShowcaseLibrary({
  open,
  onClose,
  onAdd,
}: {
  open: boolean;
  onClose: () => void;
  onAdd: (entries: ShowcaseLibraryItem[]) => Promise<void>;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-h-[min(640px,calc(100svh-2rem))] w-[min(760px,calc(100%-2rem))] max-md:max-h-svh max-md:w-full max-md:rounded-none">
        <LibraryBody onClose={onClose} onAdd={onAdd} />
      </DialogContent>
    </Dialog>
  );
}

/** 对话框内容：每次打开才挂载，所以选择状态和列表都是新的 */
function LibraryBody({
  onClose,
  onAdd,
}: {
  onClose: () => void;
  onAdd: (entries: ShowcaseLibraryItem[]) => Promise<void>;
}) {
  const aliveRef = useAliveRef();
  const [items, setItems] = useState<ShowcaseLibraryItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [status, setStatus] = useState<LoadStatus>("loading");
  const [loadingMore, setLoadingMore] = useState(false);
  const [picked, setPicked] = useState<ReadonlySet<number>>(new Set());
  const [adding, setAdding] = useState(false);

  /** 取下一页追加到后面；失败时全局 toast 已弹，只记状态 */
  const loadNext = useCallback(
    async (target: number) => {
      try {
        const result = await getShowcaseLibrary(target, PAGE_SIZE);
        if (!aliveRef.current) return;
        setItems((prev) => (target === 1 ? result.items : [...prev, ...result.items]));
        setTotal(result.total);
        setPage(target);
        setStatus("ready");
      } catch {
        if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
      } finally {
        if (aliveRef.current) setLoadingMore(false);
      }
    },
    [aliveRef],
  );

  useEffect(() => {
    void loadNext(1);
  }, [loadNext]);

  const toggle = (id: number) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const add = async () => {
    if (picked.size === 0 || adding) return;
    setAdding(true);
    try {
      /** 按素材库里的顺序添加，和界面上看到的一致 */
      await onAdd(items.filter((item) => picked.has(item.asset_id)));
      if (aliveRef.current) onClose();
    } catch {
      // 全局 toast 已弹，对话框保持打开，已成功的那几条已经在片单里（卡片会置灰）
    } finally {
      if (aliveRef.current) setAdding(false);
    }
  };

  return (
    <>
      <DialogHeader className="flex-row items-start justify-between gap-3 border-b p-5">
        <div>
          <DialogTitle className="text-base font-semibold">从素材库添加</DialogTitle>
          <DialogDescription className="mt-0.5 text-[12.5px]">
            只列出平台生成的视频，提示词和模型会从生成记录自动带入。
          </DialogDescription>
        </div>
        <Button variant="ghost" size="icon-sm" aria-label="关闭（Esc）" onClick={onClose}>
          <X />
        </Button>
      </DialogHeader>

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {status === "loading" && (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-3">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="aspect-[4/3] rounded-xl" />
            ))}
          </div>
        )}
        {status === "error" && (
          <div className="text-muted-foreground flex flex-col items-center gap-2 py-12 text-sm">
            加载失败
            <Button variant="outline" size="sm" onClick={() => void loadNext(1)}>
              重试
            </Button>
          </div>
        )}
        {status === "ready" && items.length === 0 && (
          <div className="text-muted-foreground py-12 text-center text-sm">
            <b className="text-foreground mb-1 block text-[15px]">还没有平台生成的视频</b>
            用户生成视频后会出现在这里，也可以直接上传视频。
          </div>
        )}
        {status === "ready" && items.length > 0 && (
          <>
            <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-3">
              {items.map((item) => {
                const selected = picked.has(item.asset_id);
                const warnings = showcaseWarnings(item.byte_size, item.width, item.height);
                return (
                  <button
                    key={item.asset_id}
                    type="button"
                    aria-pressed={selected}
                    disabled={item.added}
                    onClick={() => toggle(item.asset_id)}
                    className={cn(
                      "bg-card focus-visible:ring-ring/60 relative overflow-hidden rounded-xl border text-left transition-[border-color,box-shadow] duration-120 outline-none focus-visible:ring-3",
                      "enabled:hover:border-ring disabled:opacity-55",
                      selected && "border-foreground ring-foreground ring-1",
                    )}
                  >
                    <div className="bg-muted relative aspect-video">
                      <video
                        src={`${item.video_url}#t=0.1`}
                        muted
                        playsInline
                        preload="metadata"
                        className="absolute inset-0 size-full object-cover"
                      />
                      {item.added ? (
                        <span className="bg-cover-scrim text-cover-foreground absolute top-2 left-2 inline-flex h-5 items-center rounded-md px-1.5 text-[11px]">
                          已在片单
                        </span>
                      ) : (
                        <span
                          className={cn(
                            "border-cover-foreground bg-cover-scrim absolute top-2 right-2 grid size-5.5 place-items-center rounded-full border-[1.5px] text-transparent",
                            selected && "bg-primary border-primary text-primary-foreground",
                          )}
                        >
                          <Check className="size-3.5" />
                        </span>
                      )}
                    </div>
                    <div className="px-3 pt-2.5 pb-3">
                      <p className="line-clamp-2 text-[12.5px] leading-normal">
                        {item.prompt || "（没有找到生成它的提示词）"}
                      </p>
                      <div className="text-muted-foreground mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs">
                        {item.model_label && <Tag>{item.model_label}</Tag>}
                        <span>
                          {item.owner} · {formatCanvasTime(item.created_at)}
                        </span>
                        {warnings.includes("portrait") && (
                          <span className="text-status-warning inline-flex items-center gap-1">
                            <TriangleAlert className="size-3.5" />
                            竖屏
                          </span>
                        )}
                      </div>
                    </div>
                  </button>
                );
              })}
            </div>
            {items.length < total && (
              <div className="mt-4 flex justify-center">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={loadingMore}
                  onClick={() => {
                    setLoadingMore(true);
                    void loadNext(page + 1);
                  }}
                >
                  {loadingMore && <Loader2 className="animate-spin" />}
                  加载更多
                </Button>
              </div>
            )}
          </>
        )}
      </div>

      <DialogFooter className="flex-row items-center justify-between border-t p-4">
        <span className="text-muted-foreground text-[13px] tabular-nums">
          {picked.size > 0 ? `已选 ${picked.size} 个` : "未选择"}
        </span>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={onClose} disabled={adding}>
            取消
          </Button>
          <Button disabled={picked.size === 0 || adding} onClick={() => void add()}>
            {adding && <Loader2 className="animate-spin" />}
            {picked.size > 0 ? `添加 ${picked.size} 个` : "添加"}
          </Button>
        </div>
      </DialogFooter>
    </>
  );
}
