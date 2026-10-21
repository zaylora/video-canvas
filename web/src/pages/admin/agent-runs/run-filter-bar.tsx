import { RotateCcw } from "lucide-react";
import { useState } from "react";

import type { AgentStatsGroup } from "@/api/admin/agent-run/type.d";
import { FilterSelect } from "@/components/admin-ui/filter-select";
import { TableToolbar } from "@/components/admin-ui/table-toolbar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  RUN_FEEDBACK_OPTIONS,
  RUN_RANGE_OPTIONS,
  RUN_STATUS_OPTIONS,
  harnessOptions,
  modelOptions,
  normalizeUserIdInput,
  type RunFeedbackChoice,
  type RunFilters,
  type RunRange,
  type RunStatusGroup,
} from "@/utils/admin/agent-run";

/** 「全部」在下拉里用空串表示（FilterSelect 约定第一项值为空）；筛选状态里用 "all" */
const ALL = "all";
const toSelect = (value: string) => (value === ALL ? "" : value);
const toOptions = (options: ReadonlyArray<readonly [string, string]>) =>
  options.map(([value, label]) => [toSelect(value), label] as const);

/**
 * 用户 ID 输入框：回车或失焦才生效（边输边查会让列表一直闪）。
 * 写了不合法的内容（非数字、0）会清空并按「不限」处理；筛选被外面重置时输入框跟着变。
 * @param value 已生效的用户 ID 文本
 * @param onCommit 生效时调用，参数是规整后的文本（不限为空串）
 */
export function UserIdInput({
  value,
  onCommit,
}: {
  value: string;
  onCommit: (value: string) => void;
}) {
  const [text, setText] = useState(value);
  const [seen, setSeen] = useState(value);
  // 已生效的值被外面改了（比如重置筛选），输入框跟着变；在渲染期间同步，免得先闪一下旧值
  if (value !== seen) {
    setSeen(value);
    setText(value);
  }
  const commit = () => {
    const next = normalizeUserIdInput(text);
    setText(next);
    if (next !== value) onCommit(next);
  };
  return (
    <Input
      data-slot="run-user-filter"
      inputMode="numeric"
      autoComplete="off"
      aria-label="用户 ID"
      placeholder="用户 ID"
      value={text}
      className="h-8 w-28 text-xs"
      onChange={(event) => setText(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter") commit();
      }}
    />
  );
}

/**
 * 运行记录的筛选栏：时间范围、状态、反馈、仅看被撤销、Harness 版本、模型、用户 ID，以及「重置筛选」。
 * 任一项变化都由上层重置分页回第一页。Harness 与模型的选项来自统计，当前选中的值即使不在统计里也会保留。
 * @param filters 当前筛选
 * @param active 有没有筛选在生效，没有时「重置筛选」置灰
 * @param harness 按 harness_key 分组的统计，给 Harness 下拉取选项
 * @param models 按 model_key 分组的统计，给模型下拉取选项
 */
export function RunFilterBar({
  filters,
  active,
  harness,
  models,
  onChange,
  onReset,
}: {
  filters: RunFilters;
  active: boolean;
  harness: AgentStatsGroup[];
  models: AgentStatsGroup[];
  onChange: (patch: Partial<RunFilters>) => void;
  onReset: () => void;
}) {
  return (
    <TableToolbar data-slot="run-filter-bar" role="search" aria-label="筛选运行记录">
      <FilterSelect
        aria-label="时间范围"
        value={toSelect(filters.range)}
        options={toOptions(RUN_RANGE_OPTIONS)}
        onValueChange={(v) => onChange({ range: (v || ALL) as RunRange })}
      />
      <FilterSelect
        aria-label="状态"
        value={toSelect(filters.status)}
        options={toOptions(RUN_STATUS_OPTIONS)}
        onValueChange={(v) => onChange({ status: (v || ALL) as RunStatusGroup })}
      />
      <FilterSelect
        aria-label="反馈"
        value={toSelect(filters.feedback)}
        options={toOptions(RUN_FEEDBACK_OPTIONS)}
        onValueChange={(v) => onChange({ feedback: (v || ALL) as RunFeedbackChoice })}
      />
      <FilterSelect
        aria-label="Harness 版本"
        value={filters.harnessKey}
        options={harnessOptions(harness, filters.harnessKey).map(
          ([v, l]) => [v, v ? l : "全部 Harness"] as const,
        )}
        onValueChange={(v) => onChange({ harnessKey: v })}
      />
      <FilterSelect
        aria-label="模型"
        value={filters.modelKey}
        options={modelOptions(models, filters.modelKey).map(
          ([v, l]) => [v, v ? l : "全部模型"] as const,
        )}
        onValueChange={(v) => onChange({ modelKey: v })}
      />
      <UserIdInput value={filters.userId} onCommit={(userId) => onChange({ userId })} />
      <label className="text-muted-foreground flex items-center gap-2 text-xs">
        <Switch
          size="sm"
          checked={filters.undone}
          aria-label="仅看被撤销"
          onCheckedChange={(undone) => onChange({ undone })}
        />
        仅看被撤销
      </label>
      <Button variant="ghost" size="sm" className="ml-auto" disabled={!active} onClick={onReset}>
        <RotateCcw />
        重置筛选
      </Button>
    </TableToolbar>
  );
}
