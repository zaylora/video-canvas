import { useMemo, useState } from "react";

import type { ChannelView, ConfigListItem, ConfigRevision, PluginView } from "@/api/admin-ai/type";
import { DataTablePagination } from "@/components/admin-ui/data-table-pagination";
import { DataTableToolbar } from "@/components/admin-ui/data-table-toolbar";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { SearchInput } from "@/components/admin-ui/search-input";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";

import { KIND_ORDER } from "../kind";
import { ModelRows } from "./model-rows";

/** 状态筛选（设计稿的“全部状态”下拉） */
const STATES: Array<[string, string]> = [
  ["", "全部状态"],
  ["on", "上架中"],
  ["draft", "有未发布草稿"],
  ["unpublished", "从未发布"],
];

const matchState = (item: ConfigListItem, state: string) =>
  !state ||
  (state === "on" && !!item.enabled) ||
  (state === "draft" && item.has_unpublished_draft) ||
  (state === "unpublished" && item.published_revision_no === null);

/**
 * 模型列表（设计稿样式）：卡片里是工具栏（搜索、能力分段、渠道 / 状态下拉、计数）+ 表格 + 分页。
 * 筛选与分页都在前端做；勾选状态由页面持有，给批量操作用。
 */
export function ModelTable({
  models,
  status,
  channels,
  plugins,
  selected,
  onSelectedChange,
  busyKey,
  onEdit,
  onTest,
  onToggleEnabled,
  onRollback,
  onNew,
  onRetry,
}: {
  models: ConfigListItem[];
  status: "loading" | "ready" | "error";
  channels: ChannelView[];
  plugins: PluginView[];
  selected: string[];
  onSelectedChange: (keys: string[]) => void;
  busyKey: string | null;
  onEdit: (key: string) => void;
  onTest: (key: string) => void;
  onToggleEnabled: (key: string, enabled: boolean) => void;
  onRollback: (key: string, revision: ConfigRevision) => void;
  onNew: () => void;
  onRetry: () => void;
}) {
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState("");
  const [channel, setChannel] = useState("");
  const [state, setState] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  /** 改筛选条件后回到第一页 */
  const filter = (setter: (value: string) => void) => (value: string) => {
    setter(value);
    setPage(1);
  };

  const shown = useMemo(() => {
    const text = query.trim().toLowerCase();
    return models.filter(
      (item) =>
        (!text ||
          [item.key, item.label, item.name].some((t) => (t ?? "").toLowerCase().includes(text))) &&
        (!kind || item.kind === kind) &&
        (!channel || item.channel === channel) &&
        matchState(item, state),
    );
  }, [models, query, kind, channel, state]);

  const pageCount = Math.max(1, Math.ceil(shown.length / pageSize));
  const current = Math.min(page, pageCount);
  const rows = shown.slice((current - 1) * pageSize, current * pageSize);

  return (
    <section className="bg-card rounded-xl border">
      <DataTableToolbar>
        <SearchInput
          value={query}
          onChange={(event) => filter(setQuery)(event.target.value)}
          placeholder="搜索名称或标识"
          aria-label="搜索模型"
          className="w-full sm:w-64 [&_input]:h-8"
        />
        <Segmented aria-label="按能力筛选">
          {[["", "全部"], ...KIND_ORDER.map((k) => [k, MODEL_KIND_LABEL[k] ?? k])].map(
            ([value, label]) => (
              <SegmentedItem
                key={value}
                active={kind === value}
                onClick={() => filter(setKind)(value)}
              >
                {label}
              </SegmentedItem>
            ),
          )}
        </Segmented>
        <NativeSelect
          aria-label="按渠道筛选"
          className="h-8 w-auto text-xs"
          value={channel}
          onChange={(event) => filter(setChannel)(event.target.value)}
        >
          <option value="">全部渠道</option>
          {channels.map((item) => (
            <option key={item.key} value={item.key}>
              {item.name}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect
          aria-label="按状态筛选"
          className="h-8 w-auto text-xs"
          value={state}
          onChange={(event) => filter(setState)(event.target.value)}
        >
          {STATES.map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </NativeSelect>
        <span className="text-muted-foreground ml-auto text-xs tabular-nums">
          {shown.length} / {models.length}
        </span>
      </DataTableToolbar>

      <div className="flex flex-col gap-4 px-4 pb-4">
        <ModelRows
          models={rows}
          status={status}
          channels={channels}
          plugins={plugins}
          selected={selected}
          onSelectedChange={onSelectedChange}
          busyKey={busyKey}
          empty={
            models.length === 0 ? (
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
          onRollback={onRollback}
          onRetry={onRetry}
        />
        {shown.length > pageSize && (
          <DataTablePagination
            page={current}
            pageSize={pageSize}
            total={shown.length}
            onPageChange={setPage}
            onPageSizeChange={(size) => {
              setPageSize(size);
              setPage(1);
            }}
          />
        )}
      </div>
    </section>
  );
}
