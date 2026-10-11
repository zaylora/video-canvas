import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import type { TaskOutput } from "@/api/generation-task/type";

/**
 * 音频结果的播放弹窗：直接播放，点遮罩、按 Esc 或右上角关闭。
 * 图片和视频有画面，走画布同款的媒体灯箱（MediaLightbox），不在这里。
 * @param output 要播放的音频产出；null 表示关着
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
      <DialogContent className="flex w-fit items-center justify-center bg-black/90 p-2">
        <DialogTitle className="sr-only">{title}</DialogTitle>
        {output?.url && (
          <audio src={output.url} controls autoPlay className="m-6 w-[min(80vw,480px)]" />
        )}
      </DialogContent>
    </Dialog>
  );
}
