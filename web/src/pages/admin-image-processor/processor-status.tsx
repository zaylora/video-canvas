import type { ProcessorStatus, ProcessorView } from "@/api/admin-image-processor/type";
import { Tag, type TagTone } from "@/components/admin-ui/tag";

const STATUS_UI: Record<ProcessorStatus, { label: string; tone: TagTone }> = {
  draft: { label: "草稿", tone: "neutral" },
  published: { label: "已发布", tone: "success" },
  disabled: { label: "已停用", tone: "warning" },
};

/**
 * 处理服务状态标签：已发布且有未发布草稿时，下面多一个“有未发布草稿”标签，
 * 提醒线上配置与当前编辑的不一样，发布后才生效。
 * @param processor 处理服务视图
 */
export function ProcessorStatusTags({
  processor,
}: {
  processor: Pick<ProcessorView, "status" | "has_draft">;
}) {
  const { label, tone } = STATUS_UI[processor.status];
  return (
    <div data-slot="processor-status" className="flex flex-col items-start gap-1">
      <Tag tone={tone}>{label}</Tag>
      {processor.has_draft && <Tag tone="violet">有未发布草稿</Tag>}
    </div>
  );
}
