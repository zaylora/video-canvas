import { useRef, useState } from "react";
import { Plus } from "lucide-react";
import { useSearchParams } from "react-router";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";

import { useRetained } from "@/hooks/use-retained";
import { publishBlockReason, resolveModelChannel } from "@/utils/admin/model-channel";

import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { useAdminOutlet } from "../../use-admin";
import { ModelTestDialog } from "./model-test-dialog";
import { ModelDialog, type ModelDialogHandle } from "./model-dialog";
import { BatchBar } from "./batch-bar";
import { useModelBatch } from "./use-model-batch";
import { useModelRowActions } from "./use-model-row-actions";
import { ModelTable } from "./model-table";
import { PublishDialog, RollbackDialog } from "./publish-dialog";
import { useModelWorkspace } from "./use-model-workspace";

/**
 * 模型页：左侧模型列表，中间编辑器（表单 / JSON），右侧结果面板（问题 / 请求描述 / 试跑 / 追踪）。
 * 选中的模型在 URL 里（?key=<key>；/models/new 是新建，?from=import&draft=<id> 带入导入草稿），
 * 状态与动作都在 useModelWorkspace，这里只负责拼装与三个确认框（放弃修改、发布、回滚）。
 */
export default function ModelsPage() {
  const { catalog } = useAdminOutlet();
  const ws = useModelWorkspace(catalog);
  const [params] = useSearchParams();
  // 列表里点“测试”会带 ?test=1：只弹测试弹窗，不显示编辑弹窗
  const testOnly = params.get("test") === "1" && ws.selection === "model";
  const editorRef = useRef<ModelDialogHandle>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const batch = useModelBatch(ws.models, ws.reloadList);
  const row = useModelRowActions(catalog, ws.reloadList);
  // 列表刷新后丢掉已经不存在的勾选
  const checked = selected.filter((key) => ws.models.some((item) => item.key === key));

  // 关闭时 rollbackTarget 置空，确认框里的渠道摘要按最后一个目标算，留到退出动画播完
  const shownRollback = useRetained(ws.rollbackTarget);
  const rollbackInfo = resolveModelChannel(
    shownRollback?.body_json,
    catalog.channels,
    catalog.plugins,
  );
  const rollbackBlock = shownRollback
    ? publishBlockReason(rollbackInfo, catalog.channelsStatus === "ready")
    : null;

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>模型</PageHeaderTitle>
          </PageHeaderHeading>
          <PageHeaderActions>
            <Button onClick={ws.startNew}>
              <Plus />
              新建模型
            </Button>
          </PageHeaderActions>
        </PageHeader>
        <BatchBar
          selected={checked}
          channels={catalog.channels}
          batch={batch}
          onClear={() => setSelected([])}
        />
        <ModelTable
          models={ws.models}
          status={ws.listState}
          channels={catalog.channels}
          plugins={catalog.plugins}
          selected={checked}
          onSelectedChange={setSelected}
          busyKey={row.busyKey}
          onEdit={(key) => ws.selectModel(key)}
          onTest={(key) => ws.selectModel(key, { test: true })}
          onToggleEnabled={(key, enabled) => void row.toggleEnabled(key, enabled)}
          onRollback={row.requestRollback}
          onDelete={row.requestDelete}
          channelsReady={catalog.channelsStatus === "ready" && catalog.pluginsStatus === "ready"}
          initialState={params.get("status") ?? ""}
          onNew={ws.startNew}
          onRetry={() => void ws.reloadList()}
        />

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
            <ModelDialog ref={editorRef} ws={ws} catalog={catalog} />
          </DialogContent>
        </Dialog>

        <ModelTestDialog
          open={testOnly}
          ws={ws}
          onClose={ws.closeEditor}
          onClosed={ws.clearEditor}
        />

        <ConfirmDialog
          open={!!ws.pendingNav}
          title="放弃未保存的修改？"
          description="当前模型有未保存的修改，离开后这些修改会丢失。"
          confirmLabel="放弃修改"
          destructive
          onConfirm={ws.confirmNav}
          onCancel={ws.cancelNav}
        />
        <PublishDialog
          open={!!ws.publishKey}
          online={!!ws.detail?.published && !!ws.detail.enabled}
          modelKey={ws.publishKey ?? ""}
          body={ws.body}
          info={ws.info}
          busy={ws.busy === "publish"}
          error={ws.publishError}
          onConfirm={() => void ws.confirmPublish()}
          onCancel={ws.cancelPublish}
        />
        <RollbackDialog
          revision={ws.rollbackTarget}
          modelKey={ws.key ?? ""}
          info={rollbackInfo}
          blockReason={rollbackBlock}
          busy={ws.busy === "rollback"}
          error={ws.rollbackError}
          onConfirm={() => void ws.confirmRollback()}
          onCancel={ws.cancelRollback}
        />
      </main>
    </div>
  );
}
