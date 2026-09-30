import { useRef } from "react";

import { publishBlockReason, resolveModelChannel } from "@/utils/admin/model-channel";

import { ResultPanel } from "../result-panel";
import { ConfirmDialog } from "../shared";
import { useAdminOutlet } from "../use-admin";
import { ModelEditor, type ModelEditorHandle } from "./model-editor";
import { ModelList } from "./model-list";
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
  const editorRef = useRef<ModelEditorHandle>(null);

  const rollbackInfo = resolveModelChannel(
    ws.rollbackTarget?.body_json,
    catalog.channels,
    catalog.plugins,
  );
  const rollbackBlock = ws.rollbackTarget
    ? publishBlockReason(rollbackInfo, catalog.channelsStatus === "ready")
    : null;

  return (
    <div className="flex h-full min-h-0 flex-col lg:flex-row">
      <ModelList
        models={ws.models}
        status={ws.listState}
        selectedKey={ws.key}
        isNew={ws.isNew}
        channels={catalog.channels}
        onSelect={ws.selectModel}
        onNew={ws.startNew}
        onRetry={() => void ws.reloadList()}
      />
      <ModelEditor ref={editorRef} ws={ws} catalog={catalog} />
      <ResultPanel
        tab={ws.resultTab}
        onTabChange={ws.setResultTab}
        entries={ws.entries}
        dryRun={ws.dryRun}
        run={ws.run}
        trace={ws.trace}
        onClear={ws.clearResults}
        onLocate={(path) => editorRef.current?.locate(path)}
        onRefreshTrace={ws.refreshTrace}
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
    </div>
  );
}
