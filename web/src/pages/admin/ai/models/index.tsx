import { useState } from "react";
import { Plus } from "lucide-react";
import { useSearchParams } from "react-router";

import { Button } from "@/components/ui/button";

import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { useAdminOutlet } from "../../use-admin";
import { ModelEditorDialogs } from "./model-editor-dialogs";
import { useUrlModelRoute } from "./model-route";
import { BatchBar } from "./batch-bar";
import { useModelBatch } from "./use-model-batch";
import { useModelRowActions } from "./use-model-row-actions";
import { ModelTable } from "./model-table";
import { useModelWorkspace } from "./use-model-workspace";

/**
 * 模型页：左侧模型列表，中间编辑器（表单 / JSON），右侧结果面板（问题 / 请求描述 / 试跑 / 追踪）。
 * 选中的模型在 URL 里（?key=<key>；/models/new 是新建，?from=import&draft=<id> 带入导入草稿），
 * 状态与动作都在 useModelWorkspace，这里只负责拼装与放弃修改的确认框。
 */
export default function ModelsPage() {
  const { catalog } = useAdminOutlet();
  const ws = useModelWorkspace(catalog, useUrlModelRoute());
  const [params] = useSearchParams();
  // 列表里点“测试”会带 ?test=1：只弹测试弹窗，不显示编辑弹窗
  const testOnly = params.get("test") === "1" && ws.selection === "model";
  const [selected, setSelected] = useState<string[]>([]);
  const batch = useModelBatch(ws.models, ws.reloadList);
  const row = useModelRowActions(ws.reloadList);
  // 列表刷新后丢掉已经不存在的勾选
  const checked = selected.filter((key) => ws.models.some((item) => item.key === key));

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
          onDelete={row.requestDelete}
          channelsReady={catalog.channelsStatus === "ready" && catalog.pluginsStatus === "ready"}
          initialState={params.get("status") ?? ""}
          onNew={ws.startNew}
          onRetry={() => void ws.reloadList()}
        />

        <ModelEditorDialogs ws={ws} catalog={catalog} testOnly={testOnly} />
      </main>
    </div>
  );
}
