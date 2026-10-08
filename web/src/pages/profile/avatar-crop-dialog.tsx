import { useState } from "react";
import Cropper, { type Area, type Point } from "react-easy-crop";
import { Info, LoaderCircle, ZoomIn, ZoomOut } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useMeStore } from "@/store/me";
import { exportAvatar, type CropArea } from "@/utils/profile/avatar-export";
import { avatarExportName, avatarUploadError } from "@/utils/profile/profile-rules";
import { ApiError } from "@/utils/requests/request";

/** 缩放范围：1–3 倍 */
const MIN_ZOOM = 1;
const MAX_ZOOM = 3;

/** 待裁剪的图片 */
export type CropSource = {
  /** 原图的 object URL，关闭后由调用方回收 */
  url: string;
  /** 原图是否是 GIF：导出后只剩一帧，要提示用户 */
  gif: boolean;
};

/**
 * 头像裁剪框（react-easy-crop）：圆形遮罩、1:1、缩放 1–3 倍、拖拽、方向键微调。
 * 保存时导出 512×512 WebP（不支持则 PNG）再上传；失败不关闭，保留选区，用户可以直接重试。
 * 上传中不能关闭。取消或点遮罩外不会改动现有头像。
 * @param source 待裁剪的图片；null 表示关闭
 * @param onClose 关闭回调
 */
export function AvatarCropDialog({
  source,
  onClose,
}: {
  source: CropSource | null;
  onClose: () => void;
}) {
  const updateAvatar = useMeStore((state) => state.updateAvatar);
  const [crop, setCrop] = useState<Point>({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(MIN_ZOOM);
  const [area, setArea] = useState<CropArea | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [prevUrl, setPrevUrl] = useState(source?.url);

  /** 换了一张图：选区、缩放和错误都重置 */
  if (prevUrl !== source?.url) {
    setPrevUrl(source?.url);
    setCrop({ x: 0, y: 0 });
    setZoom(MIN_ZOOM);
    setArea(null);
    setError("");
  }

  const save = async () => {
    if (!source || !area || saving) return;
    setSaving(true);
    setError("");
    try {
      const blob = await exportAvatar(source.url, area);
      await updateAvatar(blob, avatarExportName(blob.type));
      toast.success("头像已更新");
      onClose();
    } catch (err) {
      setError(
        err instanceof ApiError
          ? avatarUploadError(err.code, err.message)
          : avatarUploadError(undefined, err instanceof Error ? err.message : ""),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open={!!source}
      onOpenChange={(open) => {
        if (!open && !saving) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md" showCloseButton={!saving}>
        <DialogHeader>
          <DialogTitle>裁剪头像</DialogTitle>
          <DialogDescription>
            拖动调整位置，滚轮或滑块缩放；聚焦图片后可用方向键微调。
          </DialogDescription>
        </DialogHeader>
        {source && (
          <div className="bg-muted relative aspect-square w-full overflow-hidden rounded-lg">
            <Cropper
              image={source.url}
              crop={crop}
              zoom={zoom}
              minZoom={MIN_ZOOM}
              maxZoom={MAX_ZOOM}
              aspect={1}
              cropShape="round"
              showGrid={false}
              onCropChange={setCrop}
              onZoomChange={setZoom}
              onCropComplete={(_: Area, pixels: Area) => setArea(pixels)}
              cropperProps={{ "aria-label": "头像裁剪区域，方向键微调" }}
            />
          </div>
        )}
        <label className="text-muted-foreground flex items-center gap-2">
          <ZoomOut className="size-4 shrink-0" aria-hidden />
          <input
            type="range"
            min={MIN_ZOOM}
            max={MAX_ZOOM}
            step={0.01}
            value={zoom}
            disabled={saving}
            aria-label="缩放"
            onChange={(event) => setZoom(Number(event.target.value))}
            className="accent-primary h-1.5 flex-1 cursor-pointer"
          />
          <ZoomIn className="size-4 shrink-0" aria-hidden />
        </label>
        {source?.gif && (
          <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
            <Info className="size-3.5" />
            动图会保存为静态图
          </p>
        )}
        {error && (
          <p role="alert" className="text-destructive text-xs">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" disabled={saving} onClick={onClose}>
            取消
          </Button>
          <Button disabled={saving || !area} onClick={() => void save()}>
            {saving && <LoaderCircle className="animate-spin" />}
            {saving ? "上传中" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
