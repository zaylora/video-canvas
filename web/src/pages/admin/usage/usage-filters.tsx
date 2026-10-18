import { X } from "lucide-react";
import { useEffect, useState } from "react";

import { listModelOptions } from "@/api/admin/ai";
import type { UsageKind, UsageStatusFilter } from "@/api/admin/usage/type";
import {
  FilterPopover,
  FilterPopoverContent,
  FilterPopoverField,
  FilterPopoverTrigger,
} from "@/components/admin-ui/filter-popover";
import { ListSearch } from "@/components/admin-ui/list-search";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { ToggleChip } from "@/components/admin-ui/toggle-chip";
import { Switch } from "@/components/ui/switch";
import {
  USAGE_KIND_OPTIONS,
  USAGE_RANGES,
  moreFilterCount,
  segmentValue,
  type RangeKey,
  type UsageFilters,
} from "@/utils/admin/log-rules";

/** 模型下拉的选项：全部模型 + 已有模型（值是模型 key，文字取展示名） */
function useModelOptions() {
  const [options, setOptions] = useState<ReadonlyArray<readonly [string, string]>>([
    ["", "全部模型"],
  ]);
  useEffect(() => {
    let alive = true;
    listModelOptions()
      .then((models) => {
        if (!alive) return;
        setOptions([
          ["", "全部模型"],
          ...models.map((m) => [m.key, m.label || m.name || m.key] as const),
        ]);
      })
      // 模型列表拿不到不影响看记录，保留“全部模型”
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);
  return options;
}

/** 状态分段的选项：取消、超时从概览卡进入 */
const STATUS_SEGMENTS: ReadonlyArray<readonly [UsageStatusFilter, string]> = [
  ["all", "全部"],
  ["succeeded", "成功"],
  ["failed", "失败"],
  ["active", "进行中"],
];

/**
 * 使用记录的筛选条：搜索框、状态分段（全部 / 成功 / 失败 / 进行中）、时间范围、「更多筛选」弹层
 * （类型、模型、含试跑）、可清除的用户筛选标签。窄屏（<640px）纵向堆叠。
 * @param userName 用户筛选标签上的用户名，还不知道时显示 #id
 */
function UsageFilterBar({
  filters,
  userName,
  onChange,
}: {
  filters: UsageFilters;
  userName: string | undefined;
  onChange: (patch: Partial<UsageFilters>) => void;
}) {
  const modelOptions = useModelOptions();
  const selected = segmentValue(filters.status);
  return (
    <div data-slot="usage-filters" className="flex flex-col gap-2">
      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <ListSearch
          value={filters.q}
          onCommit={(q) => onChange({ q })}
          placeholder="找人：输入用户名或邮箱，也可以粘贴任务编号"
          className="sm:w-96"
        />
        <Segmented aria-label="状态筛选" className="self-start">
          {STATUS_SEGMENTS.map(([value, label]) => (
            <SegmentedItem
              key={value}
              slideId="usage-status-filter"
              active={selected === value}
              onClick={() => onChange({ status: value })}
            >
              {label}
            </SegmentedItem>
          ))}
        </Segmented>
        <div className="flex flex-wrap items-center gap-2">
          <OptionSelect
            aria-label="时间范围"
            size="sm"
            className="w-auto text-xs"
            value={filters.range}
            onValueChange={(value) => onChange({ range: value as RangeKey })}
            options={USAGE_RANGES.map(([value, label]) => ({ value, label }))}
          />
          <FilterPopover>
            <FilterPopoverTrigger count={moreFilterCount(filters)}>更多筛选</FilterPopoverTrigger>
            <FilterPopoverContent>
              <FilterPopoverField label="类型">
                <div className="flex flex-wrap gap-1.5">
                  {USAGE_KIND_OPTIONS.map(([value, label]) => (
                    <ToggleChip
                      key={value}
                      pressed={filters.kind === value}
                      className="h-7 px-2.5"
                      onClick={() => onChange({ kind: value as UsageKind | "" })}
                    >
                      {label.replace("全部类型", "全部")}
                    </ToggleChip>
                  ))}
                </div>
              </FilterPopoverField>
              <FilterPopoverField label="模型">
                <OptionSelect
                  aria-label="模型筛选"
                  size="sm"
                  className="w-full text-xs"
                  value={filters.model}
                  onValueChange={(model) => onChange({ model })}
                  options={modelOptions.map(([value, label]) => ({ value, label }))}
                />
              </FilterPopoverField>
              <label className="flex items-center gap-2 text-xs">
                <Switch
                  size="sm"
                  checked={filters.includeTest}
                  onCheckedChange={(includeTest) => onChange({ includeTest })}
                />
                同时显示运营试跑的任务
              </label>
            </FilterPopoverContent>
          </FilterPopover>
        </div>
      </div>
      {filters.userId !== null && (
        <div>
          <span className="bg-muted inline-flex items-center gap-1 rounded-md py-0.5 pr-0.5 pl-2 text-xs">
            {userName ? `用户：${userName} · #${filters.userId}` : `用户 #${filters.userId}`}
            <button
              type="button"
              aria-label="清除用户筛选"
              className="text-muted-foreground hover:bg-background hover:text-foreground focus-visible:ring-ring/50 inline-flex size-5 items-center justify-center rounded outline-none focus-visible:ring-2"
              onClick={() => onChange({ userId: null })}
            >
              <X className="size-3.5" />
            </button>
          </span>
        </div>
      )}
    </div>
  );
}

export { UsageFilterBar };
