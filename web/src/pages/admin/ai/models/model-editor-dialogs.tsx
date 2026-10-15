import { useRef } from "react";

import type { ModelListItem } from "@/api/admin/ai/type.d";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";

import { ModelDialog, type ModelDialogHandle } from "./model-dialog";
import { ModelTestDialog } from "./model-test-dialog";
import type { ModelCatalog, ModelWorkspace } from "./use-model-workspace";

/**
 * 模型编辑弹窗 + 测试弹窗 + 放弃修改的确认框，由模型管理面板（channel-models-panel）挂载，
 * 选中状态来自 ws 绑定的路由（见 model-route）。testOnly 为 true 时只显示测试弹窗。
 */
export function ModelEditorDialogs({
  ws,
  catalog,
  testOnly = false,
  seed,
}: {
  seed?: ModelListItem;
  ws: ModelWorkspace;
  catalog: ModelCatalog;
  testOnly?: boolean;
}) {
  const editorRef = useRef<ModelDialogHandle>(null);
  return (
    <>
      <Dialog
        open={ws.selection !== "none" && !testOnly}
        onOpenChange={(open) => {
          if (!open) ws.closeEditor();
        }}
        onOpenChangeComplete={(open) => {
          if (!open) ws.clearEditor();
        }}
      >
        <DialogContent
          showCloseButton={false}
          className="flex h-[min(92svh,920px)] max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl"
        >
          <ModelDialog ref={editorRef} ws={ws} catalog={catalog} seed={seed} />
        </DialogContent>
      </Dialog>

      <ModelTestDialog open={testOnly} ws={ws} onClose={ws.closeEditor} onClosed={ws.clearEditor} />

      <ConfirmDialog
        open={!!ws.pendingNav}
        title="放弃未保存的修改？"
        description="当前模型有未保存的修改，离开后这些修改会丢失。"
        confirmLabel="放弃修改"
        destructive
        onConfirm={ws.confirmNav}
        onCancel={ws.cancelNav}
      />
    </>
  );
}
