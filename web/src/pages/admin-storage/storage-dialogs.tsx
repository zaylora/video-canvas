import { toast } from "sonner";

import { deleteStorage, getStorageDeleteCheck, setDefaultStorage } from "@/api/admin-storage";
import type { StorageView } from "@/api/admin-storage/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { formatCount } from "@/utils/admin/storage-rules";

/**
 * 设为默认的确认框：写明影响范围——新文件写进新默认，已有素材不移动，进行中的生成任务产物写入新默认。
 * @param target 要设为默认的存储
 * @param current 当前的默认存储（用来写“已有 N 个素材不会移动”）；找不到时为空
 * @param onDone 切换成功后（刷新列表）；失败也会调用一次，因为失败常见原因是列表里的测试结果已过期
 */
export function confirmSetDefault(
  target: StorageView,
  current: StorageView | undefined,
  onDone: () => void,
) {
  return confirm({
    title: `把「${target.name}」设为默认？`,
    confirmLabel: "设为默认",
    description: (
      <ul className="text-muted-foreground mt-1 list-disc space-y-1 pl-5 text-left">
        <li>
          之后<b className="text-foreground">新上传和新生成</b>的素材写入「{target.name}」
        </li>
        <li>
          {current ? (
            <>
              「{current.name}」里已有的{" "}
              <b className="text-foreground tabular-nums">{formatCount(current.asset_count)}</b>{" "}
              个素材
            </>
          ) : (
            <>已有素材</>
          )}
          <b className="text-foreground">不会移动</b>，仍从原处读取，画布不受影响
        </li>
        <li>正在进行中的生成任务，产物会写入新的默认存储</li>
      </ul>
    ),
    onConfirm: async () => {
      try {
        await setDefaultStorage(target.id);
      } catch (error) {
        onDone();
        throw error;
      }
      toast.success("默认存储已切换，新素材将写入这里");
      onDone();
    },
  });
}

/**
 * 删除流程：先调删除预检。不可删时展示后端给的原因（框里禁用确认）；
 * 可删时弹确认框，说明桶里的文件不会被清理、密钥一并删除。
 * @param target 要删除的存储
 * @param onDone 删除成功后（刷新列表）
 */
export async function requestDeleteStorage(target: StorageView, onDone: () => void) {
  const check = await getStorageDeleteCheck(target.id);
  /* 只等预检；确认框打开后就返回，行上的“预检中”状态不用等用户点完 */
  if (!check.deletable) {
    void confirm({
      title: `无法删除「${target.name}」`,
      destructive: true,
      confirmLabel: "删除",
      blockReason: check.reason || "该存储当前不能删除",
      description: check.pending_uploads > 0 && (
        <>
          还有 <span className="tabular-nums">{formatCount(check.pending_uploads)}</span>{" "}
          个进行中的上传在使用它，等它们完成或过期后再试。
        </>
      ),
    });
    return;
  }
  void confirm({
    title: `删除「${target.name}」？`,
    destructive: true,
    confirmLabel: "删除",
    description: (
      <>
        该存储没有任何素材引用。删除后加密保存的密钥一并删除，桶里的文件
        <b className="text-foreground">不会</b>被清理。
      </>
    ),
    onConfirm: async () => {
      await deleteStorage(target.id);
      toast.success(`已删除「${target.name}」`);
      onDone();
    },
  });
}
