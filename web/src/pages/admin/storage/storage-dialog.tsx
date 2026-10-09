import { useState } from "react";

import type { StoragePreset, StorageView } from "@/api/admin/storage/type.d";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { useRetained } from "@/hooks/use-retained";
import { StorageForm } from "./storage-dialog-form";

/** 弹窗打开的对象：新建，或按 ID 编辑已有存储 */
export type StorageDialogTarget = { kind: "new" } | { kind: "edit"; id: number };

/**
 * 存储弹窗：新建 / 编辑；admin 打开为只读（写操作区不渲染）。
 * 版本冲突（别人先改了）时重新拉取该存储并重置表单；已有素材时定位字段锁定。
 * @param target 打开的对象，为空表示关闭
 * @param storages 当前列表（编辑时按 ID 取视图，替换凭证后也靠它拿到最新版本）
 * @param presets 服务商预设
 * @param canWrite 是否有写权限
 * @param onSaved 创建 / 更新成功后（页面负责刷新列表并关闭弹窗）
 * @param onChanged 某条存储在弹窗里变了（测试结果、替换凭证、冲突后重新拉取），用最新视图替换列表里的那条
 * @param onClose 请求关闭
 */
export function StorageDialog({
  target,
  storages,
  presets,
  canWrite,
  onSaved,
  onChanged,
  onClose,
}: {
  target: StorageDialogTarget | null;
  storages: StorageView[];
  presets: StoragePreset[];
  canWrite: boolean;
  onSaved: () => void;
  onChanged: (view: StorageView) => void;
  onClose: () => void;
}) {
  const [dirty, setDirty] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);
  /** 冲突后重新拉取：换 key 让表单按最新内容重新初始化 */
  const [epoch, setEpoch] = useState(0);
  const shown = useRetained(target);
  const storage =
    shown?.kind === "edit" ? storages.find((item) => item.id === shown.id) : undefined;

  const requestClose = () => {
    if (dirty) setConfirmClose(true);
    else onClose();
  };

  return (
    <>
      <Dialog open={!!target} onOpenChange={(next) => !next && requestClose()}>
        <DialogContent className="flex h-[min(90svh,880px)] max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-xl">
          {shown && (shown.kind === "new" || storage) && (
            <StorageForm
              key={`${shown.kind === "edit" ? shown.id : "new"}:${epoch}`}
              storage={storage ?? null}
              presets={presets}
              canWrite={canWrite}
              onSaved={onSaved}
              onChanged={onChanged}
              onConflict={() => setEpoch((value) => value + 1)}
              onDirtyChange={setDirty}
              onClose={requestClose}
            />
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={confirmClose}
        title="放弃未保存的修改？"
        description="关闭后，这次对存储配置的修改不会保存。"
        confirmLabel="放弃修改"
        destructive
        onConfirm={() => {
          setConfirmClose(false);
          setDirty(false);
          onClose();
        }}
        onCancel={() => setConfirmClose(false)}
      />
    </>
  );
}
