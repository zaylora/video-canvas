import { useState } from "react";

import type { ProcessorPreset, ProcessorView } from "@/api/admin-image-processor/type";
import type { StorageView } from "@/api/admin-storage/type";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { useRetained } from "@/hooks/use-retained";

import { ProcessorWizard, type WizardStep } from "./processor-wizard";

/** 抽屉打开的对象：新建（可预选存储），或按 ID 编辑已有处理服务（可直达某一步）。nonce 每次打开递增，用来重置向导 */
export type ProcessorSheetTarget = { nonce: number } & (
  | { kind: "new"; storageId?: number }
  | { kind: "edit"; id: number; step?: WizardStep }
);

/**
 * 处理服务抽屉：包四步向导；关闭时有未保存修改先确认。
 * @param target 打开的对象，为空表示关闭
 * @param processors 全部处理服务（编辑时按 ID 取视图，也用来判定存储占用）
 * @param presets 厂商预设
 * @param storages 全部存储
 * @param canWrite 是否有写权限
 * @param onChanged 处理服务在抽屉里变了，用最新视图更新列表
 * @param onPublished 发布成功后（页面负责刷新列表并关闭抽屉）
 * @param onClose 请求关闭
 */
export function ProcessorSheet({
  target,
  processors,
  presets,
  storages,
  canWrite,
  onChanged,
  onPublished,
  onClose,
}: {
  target: ProcessorSheetTarget | null;
  processors: ProcessorView[];
  presets: ProcessorPreset[];
  storages: StorageView[];
  canWrite: boolean;
  onChanged: (view: ProcessorView) => void;
  onPublished: () => void;
  onClose: () => void;
}) {
  const [dirty, setDirty] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);
  const shown = useRetained(target);
  const processor =
    shown?.kind === "edit" ? processors.find((item) => item.id === shown.id) : undefined;

  const requestClose = () => {
    if (dirty) setConfirmClose(true);
    else onClose();
  };

  return (
    <>
      <Sheet open={!!target} onOpenChange={(next) => !next && requestClose()}>
        <SheetContent className="w-full data-[side=right]:sm:max-w-xl">
          {shown && (shown.kind === "new" || processor) && (
            <ProcessorWizard
              key={shown.nonce}
              initial={processor ?? null}
              startStorageId={shown.kind === "new" ? shown.storageId : undefined}
              startStep={shown.kind === "edit" ? shown.step : undefined}
              presets={presets}
              storages={storages}
              processors={processors}
              canWrite={canWrite}
              onChanged={onChanged}
              onPublished={onPublished}
              onDirtyChange={setDirty}
              onClose={requestClose}
            />
          )}
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={confirmClose}
        title="放弃未保存的修改？"
        description="关闭后，这次对处理服务的修改不会保存。已经保存的草稿不受影响。"
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
