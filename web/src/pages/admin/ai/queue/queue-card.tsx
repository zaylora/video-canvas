import { Loader2, Pause, Play, RotateCcw } from "lucide-react";

import type { QueueInfo } from "@/api/admin/queue/type";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Slider } from "@/components/ui/slider";
import { cn } from "@/lib/utils";
import { countCells, isDraftDirty, shownConcurrency } from "@/utils/admin/queue";

/**
 * 「生成任务」卡片：运行 / 暂停状态、四个计数、并发滑杆、保存 / 还原、暂停 / 恢复。
 * 一个生成任务从提交、查询到转存全程占一个并发名额。
 * - 滑杆显示草稿值，草稿由页面保存，刷新带来的新服务端值不会覆盖正在拖动的草稿；
 * - 没有写权限时滑杆和按钮禁用，原因用 tooltip 说明（不隐藏）；
 * - 保存失败的原因就地显示在卡片里（全局 toast 之外的补充）；保存成功显示“已生效于 N/M 个实例”。
 * @param queue 队列概况
 * @param draft 滑杆草稿，undefined 表示没动过
 * @param saving 并发保存中
 * @param toggling 暂停 / 恢复请求中
 * @param denyReason 写操作被禁用的原因；可写为 null
 * @param error 最近一次保存失败的就地提示
 * @param applied 最近一次保存成功的“已生效”说明
 * @param onDraft 拖动滑杆
 * @param onSave 点保存
 * @param onReset 点还原
 * @param onTogglePause 点暂停 / 恢复
 */
export function QueueCard({
  queue,
  draft,
  saving,
  toggling,
  denyReason,
  error,
  applied,
  onDraft,
  onSave,
  onReset,
  onTogglePause,
}: {
  queue: QueueInfo;
  draft: number | undefined;
  saving: boolean;
  toggling: boolean;
  denyReason: string | null;
  error: string | null;
  applied: string | null;
  onDraft: (value: number) => void;
  onSave: () => void;
  onReset: () => void;
  onTogglePause: () => void;
}) {
  const shown = shownConcurrency(draft, queue.concurrency);
  const dirty = isDraftDirty(draft, queue.concurrency);
  const locked = denyReason !== null;

  return (
    <Card data-slot="queue-card" size="sm" aria-label="生成任务">
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">生成任务</h2>
          <Tag tone={queue.paused ? "warning" : "success"}>
            {queue.paused ? "已暂停" : "运行中"}
          </Tag>
        </div>

        <dl className="grid grid-cols-2 gap-1.5 sm:grid-cols-4">
          {countCells(queue.counts).map(({ key, label, value, warn }) => (
            <div key={key} className="bg-muted flex flex-col-reverse rounded-lg px-2 py-1.5">
              <dt className="text-muted-foreground text-[11px] leading-tight">{label}</dt>
              <dd
                className={cn(
                  "text-base font-semibold tabular-nums",
                  warn && value > 0 && "text-amber-600 dark:text-amber-400",
                )}
              >
                {value}
              </dd>
            </div>
          ))}
        </dl>

        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-baseline gap-x-1.5 text-xs">
            <span>并发</span>
            <b className="text-sm font-semibold tabular-nums">{shown}</b>
            <span className="text-muted-foreground tabular-nums">
              / 硬上限 {queue.hard_cap}（每实例）
            </span>
          </div>
          <ReasonTooltip reason={denyReason} className="flex w-full px-1 py-1">
            <Slider
              aria-label="生成任务并发"
              min={1}
              max={queue.hard_cap}
              step={1}
              value={shown}
              disabled={locked || saving}
              onValueChange={(value) => onDraft(Array.isArray(value) ? (value[0] ?? shown) : value)}
            />
          </ReasonTooltip>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <ReasonTooltip reason={denyReason}>
            <Button size="sm" disabled={locked || !dirty || saving} onClick={onSave}>
              {saving && <Loader2 className="animate-spin" />}
              {saving ? "保存中…" : "保存"}
            </Button>
          </ReasonTooltip>
          {dirty && !locked && (
            <Button size="sm" variant="ghost" disabled={saving} onClick={onReset}>
              <RotateCcw />
              还原
            </Button>
          )}
          <ReasonTooltip reason={denyReason} className="ml-auto">
            <Button
              size="sm"
              variant="outline"
              disabled={locked || toggling}
              onClick={onTogglePause}
            >
              {toggling ? (
                <Loader2 className="animate-spin" />
              ) : queue.paused ? (
                <Play />
              ) : (
                <Pause />
              )}
              {queue.paused ? "恢复" : "暂停"}
            </Button>
          </ReasonTooltip>
        </div>

        {error ? (
          <p role="alert" className="text-destructive text-xs">
            {error}
          </p>
        ) : applied ? (
          <p role="status" className="text-status-success text-xs">
            {applied}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}
