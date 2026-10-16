import { useMemo, type ReactNode } from "react";

import type { AssetDto } from "@/api/asset/type";
import { MediaLightbox } from "@/components/canvas/media-lightbox";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import type { PreviewItem } from "@/utils/canvas/preview-items";

/** 预览里的一项：素材 + 展示用的名字 */
export type AssetPreviewEntry = {
  /** 稳定标识，翻页、列表变化后回退都靠它（用素材 ID） */
  id: string;
  /** 素材 */
  asset: AssetDto;
  /** 名字：下载文件名兜底和读屏说明用，一般是提示词或文件名 */
  label: string;
};

/**
 * 资产页的只读预览：图片、视频复用画布同款的 MediaLightbox（翻页、缩略图条、下载都有，
 * 不传 onLocate，所以没有「定位到节点」）；音频没有画面，用小弹窗直接播放。
 * 受控：弹层状态在调用方。
 * @param entries 当前列表的全部素材，顺序即翻页顺序；音频不进灯箱翻页
 * @param targetId 正在看的素材 ID；null 表示关着
 * @param onTargetChange 翻页后回调
 * @param onClose 关闭
 * @param renderAction 灯箱底部额外的操作（如「加入资产库」），拿到当前项的素材
 */
export function AssetPreview({
  entries,
  targetId,
  onTargetChange,
  onClose,
  renderAction,
}: {
  entries: AssetPreviewEntry[];
  targetId: string | null;
  onTargetChange: (id: string) => void;
  onClose: () => void;
  renderAction?: (asset: AssetDto) => ReactNode;
}) {
  const items = useMemo<PreviewItem[]>(
    () =>
      entries.flatMap(({ id, asset, label }) =>
        asset.kind === "audio"
          ? []
          : [
              {
                id,
                src: asset.url,
                mediaType: asset.kind,
                label,
                fileName: asset.fileName ?? undefined,
              },
            ],
      ),
    [entries],
  );
  const audio = targetId
    ? entries.find((entry) => entry.id === targetId && entry.asset.kind === "audio")
    : undefined;
  /** 目标对象要稳定：灯箱靠引用判断「是不是换了目标」 */
  const lightboxTarget = useMemo(
    () => (audio || !targetId ? null : { id: targetId, origin: null }),
    [audio, targetId],
  );
  const byId = useMemo(() => new Map(entries.map((entry) => [entry.id, entry])), [entries]);

  return (
    <>
      <MediaLightbox
        items={items}
        target={lightboxTarget}
        countLabel="本页"
        onActiveChange={onTargetChange}
        onClose={onClose}
        renderAction={
          renderAction
            ? (item) => {
                const entry = byId.get(item.id);
                return entry ? renderAction(entry.asset) : null;
              }
            : undefined
        }
      />
      <Dialog open={audio !== undefined} onOpenChange={(open) => !open && onClose()}>
        <DialogContent className="flex w-fit items-center justify-center bg-black/90 p-2">
          <DialogTitle className="sr-only">{audio?.label ?? "音频预览"}</DialogTitle>
          {audio && (
            <audio src={audio.asset.url} controls autoPlay className="m-6 w-[min(80vw,480px)]" />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
