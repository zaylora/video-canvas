import type { ChannelOption, ModelListItem } from "@/api/admin/ai/type.d";
import {
  DataPanel,
  DataPanelBody,
  DataPanelPagination,
} from "@/components/admin-ui/data-panel";
import { ListSearch } from "@/components/admin-ui/list-search";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { MODEL_STATUS_LABEL } from "@/utils/admin/health";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";

import type { useAdminList } from "../../use-admin-list";
import { KIND_ORDER } from "../kind";
import { ModelRows } from "./model-rows";

/** 状态筛选：与状态列一致（在线 / 不可用 / 未上线） */
const STATES: Array<[string, string]> = [
  ["", "全部状态"],
  ["online", MODEL_STATUS_LABEL.online],
  ["broken", MODEL_STATUS_LABEL.broken],
  ["offline", MODEL_STATUS_LABEL.offline],
];

/**
 * 模型列表（设计稿样式）：卡片里是工具栏（搜索、能力分段、渠道 / 状态下拉、计数）+ 表格 + 分页。
 * 搜索、筛选、分页都在服务端（list），每行的状态由后端算好；勾选状态由页面持有，给批量操作用。
 */
export function ModelTable({
  list,
  channels,
  selected,
  onSelectedChange,
  busyKey,
  onEdit,
  onTest,
  onToggleEnabled,
  onDelete,
  onNew,
}: {
  /** 模型分页列表（useAdminList）：当页的行、总数、搜索 / 筛选 / 翻页 */
  list: ReturnType<typeof useAdminList<ModelListItem>>;
  /** 全部渠道（options）：渠道筛选下拉要用 */
  channels: ChannelOption[];
  selected: string[];
  onSelectedChange: (keys: string[]) => void;
  busyKey: string | null;
  onEdit: (key: string) => void;
  onTest: (key: string) => void;
  onToggleEnabled: (key: string, enabled: boolean) => void;
  onDelete: (item: ModelListItem) => void;
  onNew: () => void;
}) {
  const kind = list.filters.kind ?? "";

  return (
    <DataPanel>
      <div className="flex flex-wrap items-center gap-2 p-4">
        <ListSearch
          value={list.q}
          onCommit={list.setQ}
          placeholder="搜索名称或标识"
          className="[&_input]:h-8"
        />
        <Segmented aria-label="按能力筛选">
          {[
            ["", "全部"],
            ...KIND_ORDER.map((k) => [k, MODEL_KIND_LABEL[k] ?? k]),
          ].map(([value, label]) => (
            <SegmentedItem
              key={value}
              active={kind === value}
              onClick={() => list.setFilter("kind", value)}
            >
              {label}
            </SegmentedItem>
          ))}
        </Segmented>
        <OptionSelect
          aria-label="按渠道筛选"
          size="sm"
          className="w-auto text-xs"
          value={list.filters.channel ?? ""}
          onValueChange={(value) => list.setFilter("channel", value)}
          options={[
            { value: "", label: "全部渠道" },
            ...channels.map((item) => ({ value: item.key, label: item.name })),
          ]}
        />
        <OptionSelect
          aria-label="按状态筛选"
          size="sm"
          className="w-auto text-xs"
          value={list.filters.status ?? ""}
          onValueChange={(value) => list.setFilter("status", value)}
          options={STATES.map(([value, label]) => ({ value, label }))}
        />
        <span className="text-muted-foreground ml-auto text-xs tabular-nums">
          {list.items.length} / {list.total}
        </span>
      </div>

      <DataPanelBody>
        <ModelRows
          className="rounded-none border-0"
          models={list.items}
          status={list.state}
          channels={channels}
          selected={selected}
          onSelectedChange={onSelectedChange}
          busyKey={busyKey}
          empty={
            !list.filtering ? (
              <>
                还没有模型，
                <button type="button" className="underline" onClick={onNew}>
                  新建模型
                </button>
              </>
            ) : (
              "没有匹配的模型"
            )
          }
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
    </DataPanel>
  );
}
