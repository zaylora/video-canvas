import service from "@/utils/requests/service";
import { CanvasCover } from "@/components/home/canvas-cover";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { canvasSharing, dramaSharing, type SharedDrama } from "@/api/drama/share";
import { Button } from "@/components/ui/button";

function SharedCover({ item }: { item: SharedDrama }) {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    let objectURL: string | undefined;
    const controller = new AbortController();
    if (item.cover_asset)
      void service
        .get<Blob>(`/canvas/${item.id}/reader/media/${item.cover_asset}`, undefined, {
          responseType: "blob",
          signal: controller.signal,
          silent: true,
        })
        .then(
          (blob) => {
            if (active) {
              objectURL = URL.createObjectURL(blob);
              setUrl(objectURL);
            }
          },
          () => {},
        );
    return () => {
      active = false;
      controller.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [item.id, item.cover_asset]);
  return (
    <div className="aspect-16/10 overflow-hidden rounded-lg">
      <CanvasCover id={item.id} coverUrl={url} />
    </div>
  );
}
export function SharedDramaList({
  keyword,
  canvas = false,
}: {
  keyword: string;
  canvas?: boolean;
}) {
  const [request, setRequest] = useState({ page: 1 });
  const [result, setResult] = useState<{
    request: typeof request;
    items: SharedDrama[];
    hasMore: boolean;
    error: boolean;
  }>();
  const items = result?.items ?? [];
  const busy = result?.request !== request;
  useEffect(() => {
    if (!canvas) return;
    const refresh = () => setRequest({ page: 1 });
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, [canvas]);
  useEffect(() => {
    let active = true;
    void (canvas ? canvasSharing : dramaSharing).list(keyword, request.page).then(
      (data) => {
        if (!active) return;
        const next = data.list ?? [];
        setResult((previous) => ({
          request,
          items:
            request.page === 1
              ? next
              : [
                  ...new Map(
                    [...(previous?.items ?? []), ...next].map((item) => [item.id, item]),
                  ).values(),
                ],
          hasMore: next.length > 0 && data.page * data.page_size < data.total,
          error: false,
        }));
      },
      () => {
        if (!active) return;
        setResult((previous) => ({
          request,
          items: previous?.items ?? [],
          hasMore: false,
          error: true,
        }));
      },
    );
    return () => {
      active = false;
    };
  }, [canvas, keyword, request]);
  return (
    <>
      {items.map((item) => (
        <Link
          key={item.id}
          to={`/${canvas ? "canvas" : "drama"}/${item.id}/${canvas && item.role === "editor" ? "collaboration" : "reader"}`}
          className="border-border hover:bg-chrome-hover grid min-w-0 gap-2 rounded-xl border p-5"
        >
          {canvas && <SharedCover key={item.cover_asset} item={item} />}
          <strong className="truncate">{item.title}</strong>
          <span className="text-muted-foreground text-sm">
            {canvas && item.role === "editor" ? "可协作编辑" : "仅可查看"}
          </span>
        </Link>
      ))}
      <div className="col-span-full flex justify-center py-6 text-sm text-muted-foreground">
        {busy ? (
          "加载中…"
        ) : result?.error ? (
          <Button variant="outline" onClick={() => setRequest({ page: request.page })}>
            重新加载
          </Button>
        ) : items.length === 0 ? (
          keyword ? (
            `没有找到匹配的${canvas ? "画布" : "短剧"}`
          ) : (
            `还没有人与你共享${canvas ? "画布" : "短剧"}`
          )
        ) : result?.hasMore ? (
          <Button variant="outline" onClick={() => setRequest({ page: request.page + 1 })}>
            加载更多
          </Button>
        ) : null}
      </div>
    </>
  );
}
