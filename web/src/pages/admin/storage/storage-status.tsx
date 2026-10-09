import { CircleCheck, CircleX, MinusCircle } from "lucide-react";

import type { StorageView } from "@/api/admin/storage/type.d";
import { Tag, type TagTone } from "@/components/admin-ui/tag";
import { checkStatus } from "@/utils/admin/storage-rules";

const TONE_UI = {
  success: { tag: "success", icon: CircleCheck },
  danger: { tag: "danger", icon: CircleX },
  neutral: { tag: "neutral", icon: MinusCircle },
} as const satisfies Record<string, { tag: TagTone; icon: typeof CircleCheck }>;

/**
 * 连通状态：图标加文字，不只靠颜色；失败时在下面写出原因（过长截断，完整内容放 title），正常时写测试时间。
 * @param storage 存储视图
 */
export function StorageStatus({ storage }: { storage: StorageView }) {
  const status = checkStatus(storage);
  const { tag, icon: Icon } = TONE_UI[status.tone];
  return (
    <div data-slot="storage-status" className="flex min-w-0 flex-col items-start gap-0.5">
      <Tag tone={tag}>
        <Icon />
        {status.label}
      </Tag>
      {status.detail && (
        <span
          title={status.detail}
          className={
            status.tone === "danger"
              ? "max-w-[14rem] truncate text-xs text-red-600 dark:text-red-400"
              : "text-muted-foreground text-xs"
          }
        >
          {status.detail}
        </span>
      )}
    </div>
  );
}
