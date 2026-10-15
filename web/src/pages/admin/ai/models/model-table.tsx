import type { ModelListItem } from "@/api/admin/ai/type.d";
import { DataPanelBody, DataPanelPagination } from "@/components/admin-ui/data-panel";
import { ListSearch } from "@/components/admin-ui/list-search";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { MODEL_STATUS_LABEL } from "@/utils/admin/health";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { PENDING_FILTER } from "@/utils/admin/model-list";

import type { useAdminList } from "../../use-admin-list";
import { KIND_ORDER } from "../kind";
import { ModelRows } from "./model-rows";

/** 能力筛选：全部 / 各能力 / 待配置（还没选能力或协议） */
const KIND_OPTIONS = [
  { value: "", label: "全部能力" },
  ...KIND_ORDER.map((kind) => ({ value: kind, label: MODEL_KIND_LABEL[kind] ?? kind })),
  { value: PENDING_FILTER, label: "待配置" },
];

/** 状态筛选：在线 / 不可用 / 未上线（待配置在能力筛选里） */
const STATUS_OPTIONS = [
  { value: "", label: "全部状态" },
  { value: "online", label: MODEL_STATUS_LABEL.online },
  { value: "broken", label: MODEL_STATUS_LABEL.broken },
  { value: "offline", label: MODEL_STATUS_LABEL.offline },
];

/**
 * 渠道下的模型表格：工具栏（搜索、能力、状态筛选、计数）+ 表格 + 分页。
 * 搜索、筛选、分页都在服务端（list），每行的状态由后端算好；勾选状态由面板持有，给批量操作用。
 * 每行左侧有拖动手柄：拖动排序，松手后由面板（onReorder）保存。
 */
export function ModelTable({
  list,
  onReorder,
  selected,
  onSelectedChange,
  busyKey,
  onEdit,
  onTest,
  onToggleEnabled,
  onDelete,
}: {
  /** 模型分页列表（useAdminList）：当页的行、总数、搜索 / 筛选 / 翻页 */
  list: ReturnType<typeof useAdminList<ModelListItem>>;
  /** 拖动排序松手后顺序变了：当页（筛选后）模型的新顺序 */
  onReorder: (keys: string[]) => void;
  selected: string[];
  onSelectedChange: (keys: string[]) => void;
  busyKey: string | null;
  onEdit: (key: string) => void;
  onTest: (key: string) => void;
  onToggleEnabled: (key: string, enabled: boolean) => void;
  onDelete: (item: ModelListItem) => void;
}) {
  return (
    <div className="bg-card flex min-h-64 flex-1 flex-col overflow-hidden rounded-xl border">
      <div className="flex flex-wrap items-center gap-2 p-4">
        <ListSearch
          value={list.q}
          onCommit={list.setQ}
          placeholder="搜索模型标识或显示名称"
          className="[&_input]:h-8"
        />
        <OptionSelect
          aria-label="按能力筛选"
          size="sm"
          className="w-auto text-xs"
          value={list.filters.kind ?? ""}
          onValueChange={(value) => list.setFilter("kind", value)}
          options={KIND_OPTIONS}
        />
        <OptionSelect
          aria-label="按状态筛选"
          size="sm"
          className="w-auto text-xs"
          value={list.filters.status ?? ""}
          onValueChange={(value) => list.setFilter("status", value)}
          options={STATUS_OPTIONS}
        />
        <span className="text-muted-foreground ml-auto text-xs tabular-nums">
          {list.items.length} / {list.total}
        </span>
      </div>

      <DataPanelBody>
        <ModelRows
          className="overflow-visible rounded-none border-0"
          models={list.items}
          status={list.state}
          selected={selected}
          onSelectedChange={onSelectedChange}
          busyKey={busyKey}
          onReorder={onReorder}
          empty={list.filtering ? "没有匹配的模型" : "这个渠道下还没有模型"}
          onEdit={onEdit}
          onTest={onTest}
          onToggleEnabled={onToggleEnabled}
          onDelete={onDelete}
          onRetry={list.reload}
        />
      </DataPanelBody>
      {list.state === "ready" && (
        <DataPanelPagination
          page={list.page}
          pageSize={list.pageSize}
          total={list.total}
          onPageChange={list.setPage}
          onPageSizeChange={list.setPageSize}
        />
      )}
    </div>
  );
}
