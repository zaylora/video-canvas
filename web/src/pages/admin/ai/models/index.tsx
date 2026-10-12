import { useEffect, useState } from "react";
import { Plus } from "lucide-react";
import { useSearchParams } from "react-router";

import { listModels } from "@/api/admin/ai";
import type { ModelListItem } from "@/api/admin/ai/type.d";
import { Button } from "@/components/ui/button";

import { DataPage } from "@/components/admin-ui/data-panel";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { useAdminCatalogOptions } from "../../use-admin";
import { useAdminList } from "../../use-admin-list";
import { ModelEditorDialogs } from "./model-editor-dialogs";
import { useUrlModelRoute } from "./model-route";
import { BatchBar } from "./batch-bar";
import { useModelBatch } from "./use-model-batch";
import { useModelRowActions } from "./use-model-row-actions";
import { ModelTable } from "./model-table";
import { useModelWorkspace } from "./use-model-workspace";

/** 列表认的筛选参数：能力、渠道、状态（概览页“去查看”会带 ?status=） */
const MODEL_FILTERS = ["kind", "channel", "status"] as const;

/**
 * 模型页：左侧模型列表，中间编辑器（表单 / JSON），右侧结果面板（问题 / 请求描述 / 试跑 / 追踪）。
 * 选中的模型在 URL 里（?key=<key>；/models/new 是新建，?from=import&draft=<id> 带入导入草稿），
 * 状态与动作都在 useModelWorkspace，这里只负责拼装与放弃修改的确认框。
 */
export default function ModelsPage() {
  const { catalog } = useAdminCatalogOptions();
  // 保存 / 上下线让模型变了：重新拉取当前页（由 list.reload 提供，先建 list 再把它交给工作区）
  const list = useAdminList<ModelListItem>(MODEL_FILTERS, (query) =>
    listModels(query),
  );
  const ws = useModelWorkspace(catalog, useUrlModelRoute(), list.reload);
  const [params] = useSearchParams();
  // 列表里点“测试”会带 ?test=1：只弹测试弹窗，不显示编辑弹窗
  const testOnly = params.get("test") === "1" && ws.selection === "model";
  const [selected, setSelected] = useState<string[]>([]);
  // 批量操作只作用于当前页：换页、换搜索 / 筛选条件后清空勾选
  const { q, page, filters } = list;
  const filterKey = JSON.stringify(filters);
  useEffect(() => setSelected([]), [q, page, filterKey, list.pageSize]);
  const batch = useModelBatch(list.items, list.reload);
  const row = useModelRowActions(list.reload);
  // 列表刷新后丢掉已经不存在的勾选
  const checked = selected.filter((key) =>
    list.items.some((item) => item.key === key),
  );

  return (
    <DataPage>
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
        list={list}
        channels={catalog.channels}
        selected={checked}
        onSelectedChange={setSelected}
        busyKey={row.busyKey}
        onEdit={(key) => ws.selectModel(key)}
        onTest={(key) => ws.selectModel(key, { test: true })}
        onToggleEnabled={(key, enabled) => void row.toggleEnabled(key, enabled)}
        onDelete={row.requestDelete}
        onNew={ws.startNew}
      />

      <ModelEditorDialogs ws={ws} catalog={catalog} testOnly={testOnly} />
    </DataPage>
  );
}
