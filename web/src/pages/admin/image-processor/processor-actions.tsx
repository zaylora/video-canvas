import { toast } from "sonner";

import {
  deleteImageProcessor,
  disableImageProcessor,
  rollbackImageProcessor,
} from "@/api/admin-image-processor";
import type { ProcessorView } from "@/api/admin-image-processor/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";

/**
 * 回滚确认：线上切回上一个已发布版本，当前草稿不受影响。
 * @param target 要回滚的处理服务
 * @param onDone 成功后（刷新列表）
 */
export function confirmRollback(target: ProcessorView, onDone: () => void) {
  return confirm({
    title: `回滚「${target.name}」？`,
    confirmLabel: "回滚",
    description: (
      <>
        线上配置会切回上一个已发布版本
        <b className="text-foreground tabular-nums"> v{target.previous_version}</b>
        ，当前线上版本 <span className="tabular-nums">v{target.published_version}</span> 不再生效。
      </>
    ),
    onConfirm: async () => {
      await rollbackImageProcessor(target.id);
      toast.success(`已回滚到 v${target.previous_version}`);
      onDone();
    },
  });
}

/**
 * 停用确认：写明影响——「存储」里的素材回退原图 / 占位。
 * @param target 要停用的处理服务
 * @param onDone 成功后（刷新列表）
 */
export function confirmDisable(target: ProcessorView, onDone: () => void) {
  return confirm({
    title: `停用「${target.name}」？`,
    confirmLabel: "停用",
    destructive: true,
    description: (
      <>
        停用后，「{target.storage_name}」存储里的素材回退为原图（缩略图）或占位（视频封面）。
        之后可以重新校验并发布。
      </>
    ),
    onConfirm: async () => {
      await disableImageProcessor(target.id);
      toast.success(`已停用，「${target.storage_name}」回退原图 / 占位`);
      onDone();
    },
  });
}

/**
 * 删除确认：只有草稿 / 已停用的能删，已发布的要先停用（按钮本身已不渲染）。
 * @param target 要删除的处理服务
 * @param onDone 成功后（刷新列表）
 */
export function confirmDelete(target: ProcessorView, onDone: () => void) {
  return confirm({
    title: `删除「${target.name}」？`,
    confirmLabel: "删除",
    destructive: true,
    description: <>删除后无法恢复；绑定的存储和里面的素材不受影响。</>,
    onConfirm: async () => {
      await deleteImageProcessor(target.id);
      toast.success(`已删除「${target.name}」`);
      onDone();
    },
  });
}
