import type { RecordDto } from "@/api/conversation/type";
import type { TaskView } from "@/api/generation-task/type";
import type { PreviewItem } from "@/utils/canvas/preview-items";

/**
 * 一条记录里能放进媒体灯箱的结果：成功且有地址的图片和视频，顺序和结果格子一致。
 * 音频放不进灯箱（没有画面），仍走单独的播放弹窗。
 * @param record 所在记录：提示词用来起文件名
 * @param tasks 记录里各格子的最新任务快照，顺序即格子顺序
 * @returns 灯箱的预览项；id 是任务 ID，和结果格子认任务的方式一致
 */
export function recordPreviewItems(record: RecordDto, tasks: TaskView[]): PreviewItem[] {
  const base = record.prompt.slice(0, 20) || "生成结果";
  const items: PreviewItem[] = [];
  tasks.forEach((task, index) => {
    const output = task.status === "succeeded" ? task.outputs?.[0] : undefined;
    if (!output?.url) return;
    if (output.media_type !== "image" && output.media_type !== "video") return;
    items.push({
      id: String(task.id),
      src: output.url,
      mediaType: output.media_type,
      label: `${base}-${index + 1}`,
    });
  });
  return items;
}
