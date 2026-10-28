import { useMemo, useState } from "react";
import { ImageOff } from "lucide-react";

import type { AgentAttachmentDto } from "@/api/agent/type";
import { MediaLightbox } from "@/components/canvas/media-lightbox";
import { variantUrl } from "@/utils/canvas/media-lod";
import type { PreviewItem } from "@/utils/canvas/preview-items";

/**
 * 用户消息带的图片附件：气泡上方一行缩略图，点开看大图（复用画布同款灯箱，可翻页）。
 * 缩略图用 /files/ 稳定地址的缩略图变体；素材被删、加载失败时这一格显示「图片已失效」，不影响对话。
 */
export function MessageAttachments({
  items,
  size = 48,
}: {
  items: readonly AgentAttachmentDto[];
  /** 缩略图边长（px）；排队的插话里用小一点的 */
  size?: number;
}) {
  const [openId, setOpenId] = useState<string | null>(null);
  /** 加载失败的素材 id */
  const [dead, setDead] = useState<ReadonlySet<string>>(new Set());
  const previews = useMemo<PreviewItem[]>(
    () =>
      items
        .filter((a) => !dead.has(a.asset_id))
        .map((a) => ({
          id: a.asset_id,
          src: a.url,
          mediaType: "image" as const,
          label: a.name,
          fileName: a.name,
        })),
    [items, dead],
  );
  /** 目标对象要稳定：灯箱靠引用判断「是不是换了目标」 */
  const target = useMemo(() => (openId ? { id: openId, origin: null } : null), [openId]);
  if (items.length === 0) return null;
  const box = { width: `${size}px`, height: `${size}px` };

  return (
    <>
      <div className="flex flex-wrap justify-end gap-1.5">
        {items.map((a) =>
          dead.has(a.asset_id) ? (
            <div
              key={a.asset_id}
              title="图片已失效"
              style={box}
              className="bg-foreground/6 text-muted-foreground ring-chrome-border grid place-items-center gap-0.5 rounded-lg p-1 text-center text-[9.5px] leading-tight ring-1"
            >
              <ImageOff aria-hidden className="size-3.5" />
              图片已失效
            </div>
          ) : (
            <button
              key={a.asset_id}
              type="button"
              aria-label={`查看大图：${a.name}`}
              title={a.name}
              style={box}
              onClick={() => setOpenId(a.asset_id)}
              className="ring-chrome-border focus-visible:ring-node-ring/60 relative cursor-zoom-in overflow-hidden rounded-lg ring-1 outline-none focus-visible:ring-2"
            >
              <img
                src={variantUrl(a.url, "thumb").url}
                alt={a.name}
                draggable={false}
                onError={() => setDead((prev) => new Set(prev).add(a.asset_id))}
                className="size-full object-cover"
              />
            </button>
          ),
        )}
      </div>
      <MediaLightbox
        items={previews}
        target={target}
        countLabel="本条消息"
        onActiveChange={setOpenId}
        onClose={() => setOpenId(null)}
      />
    </>
  );
}
