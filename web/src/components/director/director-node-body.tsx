import { Video } from "lucide-react";

import { NODE_META } from "@/constants/canvas";
import { cn } from "@/lib/utils";
import type { DirectorScene } from "@/types";

/**
 * 导演台节点的正文：没有缩略图时是说明文字加居中的「进入导演台」按钮；
 * 有缩略图时缩略图当背景、盖一层黑色蒙版，居中仍是进入按钮，机位标签留在底部。
 * 这里不加载 three.js，缩略图是关闭编辑器时渲染上传的图片。
 * @param onEnter 点进入按钮
 */
export function DirectorNodeBody({
  scene,
  onEnter,
}: {
  scene: DirectorScene | undefined;
  onEnter: () => void;
}) {
  const PlaceholderIcon = NODE_META.get("director")?.placeholderIcon;
  const thumb = scene?.thumb?.url;

  return (
    <div className="bg-muted relative aspect-video w-full overflow-hidden rounded-[inherit]">
      {thumb && (
        <>
          <img src={thumb} alt="" draggable={false} className="size-full object-contain" />
          <div data-mask className="absolute inset-0 bg-black/55" />
        </>
      )}
      <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 px-4 text-center">
        {!thumb && (
          <>
            {PlaceholderIcon && (
              <PlaceholderIcon className="text-muted-foreground size-10 opacity-60" />
            )}
            <p className="text-muted-foreground text-xs">在 3D 空间中设计角色、机位与镜头</p>
          </>
        )}
        <button
          type="button"
          onClick={onEnter}
          className={cn(
            "nodrag focus-visible:ring-ring inline-flex h-8 cursor-pointer items-center rounded-lg px-4 text-xs font-medium outline-none focus-visible:ring-2",
            thumb
              ? "bg-white/15 text-white ring-1 ring-white/25 backdrop-blur-sm hover:bg-white/25"
              : "bg-foreground/10 text-foreground hover:bg-foreground/15",
          )}
        >
          进入导演台
        </button>
      </div>
      {scene && scene.cameras.length > 0 && (
        <div className="pointer-events-none absolute right-2 bottom-2 left-2 flex flex-wrap gap-1">
          {scene.cameras.map((camera) => (
            <span
              key={camera.id}
              className="bg-background/80 text-foreground inline-flex h-5 items-center gap-1 rounded-full px-2 text-[11px] backdrop-blur-sm"
            >
              <Video className="size-3" />
              {camera.name}
              <span className="text-muted-foreground tabular-nums">
                {scene.duration.toFixed(1)}s
              </span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
