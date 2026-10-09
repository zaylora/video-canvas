import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import type { TaskOutput } from "@/api/generation-task/type";

/**
 * 结果预览：图片看大图，视频和音频直接播放。点遮罩、按 Esc 或右上角关闭。
 * @param output 要预览的产出；null 表示关着
 * @param title 无障碍标题，一般是提示词
 * @param onClose 关闭
 */
export function PreviewDialog({
  output,
  title,
  onClose,
}: {
  output: TaskOutput | null;
  title: string;
  onClose: () => void;
}) {
  return (
    <Dialog open={output !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[92svh] w-fit max-w-[min(94vw,1100px)] items-center justify-center bg-black/90 p-2 sm:max-w-[min(94vw,1100px)]">
        <DialogTitle className="sr-only">{title}</DialogTitle>
        {output?.url && output.media_type === "image" && (
          <img
            src={output.url}
            alt={title}
            className="max-h-[86svh] max-w-full rounded-lg object-contain"
          />
        )}
        {output?.url && output.media_type === "video" && (
          <video
            src={output.url}
            controls
            autoPlay
            playsInline
            className="max-h-[86svh] max-w-full rounded-lg"
          />
        )}
        {output?.url && output.media_type === "audio" && (
          <audio src={output.url} controls autoPlay className="m-6 w-[min(80vw,480px)]" />
        )}
      </DialogContent>
    </Dialog>
  );
}
