import { useState } from "react";
import { ArrowDownToLine, ArrowUpFromLine, Loader2, PencilLine, Rocket } from "lucide-react";

import type { ChannelView } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import {
  BulkActionButton,
  DataTableBulkActions,
} from "@/components/admin-ui/data-table-bulk-actions";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { checkDeadline } from "@/utils/admin/model-body";

import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Notice } from "@/components/admin-ui/notice";
import { batchLabel, type BatchPatch, type ModelBatch } from "./use-model-batch";

/**
 * 勾选后出现的批量操作条：批量上架 / 下架 / 发布草稿 / 批量修改。
 * 每个操作都会二次确认范围（几项），完成后弹出逐项结果。
 */
export function BatchBar({
  selected,
  channels,
  batch,
  onClear,
}: {
  selected: string[];
  channels: ChannelView[];
  batch: ModelBatch;
  onClear: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const busy = batch.running !== null;
  if (selected.length === 0 && !batch.result && !busy) return null;

  return (
    <>
      <DataTableBulkActions count={selected.length} entityName="模型" onClear={onClear}>
        <BulkActionButton
          label="批量上架"
          icon={<ArrowUpFromLine />}
          disabled={busy}
          onClick={() => void batch.setEnabled(selected, true)}
        />
        <BulkActionButton
          label="批量下架"
          icon={<ArrowDownToLine />}
          disabled={busy}
          onClick={() => void batch.setEnabled(selected, false)}
        />
        <BulkActionButton
          label="发布草稿"
          icon={<Rocket />}
          disabled={busy}
          onClick={() => void batch.publishDrafts(selected)}
        />
        <BulkActionButton
          label="批量修改"
          variant="default"
          icon={<PencilLine />}
          disabled={busy}
          onClick={() => setEditing(true)}
        />
      </DataTableBulkActions>

      {busy && (
        <div
          className="mb-3 flex items-center gap-2 rounded-lg border px-3 py-2 text-sm"
          role="status"
        >
          <Loader2 className="size-4 animate-spin" />
          {batchLabel(batch.running!)}中… {batch.progress.done} / {batch.progress.total}
        </div>
      )}

      <EditDialog
        open={editing}
        count={selected.length}
        channels={channels}
        onClose={() => setEditing(false)}
        onSubmit={(patch, publish) => {
          setEditing(false);
          void batch.edit(selected, patch, publish);
        }}
      />

      <Dialog open={!!batch.result && !busy} onOpenChange={(open) => !open && batch.clearResult()}>
        <DialogContent className="sm:max-w-lg">
          {batch.result && (
            <>
              <DialogHeader>
                <DialogTitle>{batchLabel(batch.result.action)}完成</DialogTitle>
                <DialogDescription>
                  成功 {batch.result.done.length} 项，跳过 {batch.result.skipped.length} 项，失败{" "}
                  {batch.result.failed.length} 项。
                </DialogDescription>
              </DialogHeader>
              <div className="flex max-h-72 flex-col gap-2 overflow-y-auto">
                {batch.result.failed.length > 0 && (
                  <Notice tone="danger" title="失败">
                    <ul className="mt-1 space-y-0.5">
                      {batch.result.failed.map((item) => (
                        <li key={item.key}>
                          <code className="font-mono">{item.key}</code>：{item.reason}
                        </li>
                      ))}
                    </ul>
                  </Notice>
                )}
                {batch.result.skipped.length > 0 && (
                  <Notice tone="warning" title="跳过">
                    <ul className="mt-1 space-y-0.5">
                      {batch.result.skipped.map((item) => (
                        <li key={item.key}>
                          <code className="font-mono">{item.key}</code>：{item.reason}
                        </li>
                      ))}
                    </ul>
                  </Notice>
                )}
                {batch.result.done.length > 0 && (
                  <Notice tone="success" title="成功">
                    <span className="font-mono">{batch.result.done.join("、")}</span>
                  </Notice>
                )}
              </div>
              <DialogFooter>
                <Button onClick={batch.clearResult}>知道了</Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function EditDialog({
  open,
  count,
  channels,
  onClose,
  onSubmit,
}: {
  open: boolean;
  count: number;
  channels: ChannelView[];
  onClose: () => void;
  onSubmit: (patch: BatchPatch, publish: boolean) => void;
}) {
  const [creditsOn, setCreditsOn] = useState(false);
  const [credits, setCredits] = useState("");
  const [deadlineOn, setDeadlineOn] = useState(false);
  const [deadline, setDeadline] = useState("30m");
  const [channelOn, setChannelOn] = useState(false);
  const [channel, setChannel] = useState("");
  const [publish, setPublish] = useState(false);

  const creditsValue = Number(credits);
  const creditsError =
    creditsOn && (credits.trim() === "" || !Number.isFinite(creditsValue) || creditsValue < 0)
      ? "请填写不小于 0 的数字"
      : null;
  const deadlineError = deadlineOn ? checkDeadline(deadline) : null;
  const channelError = channelOn && !channel ? "请选择渠道" : null;
  const nothing = !creditsOn && !deadlineOn && !channelOn;
  const invalid = nothing || !!creditsError || !!deadlineError || !!channelError;

  const submit = () => {
    const patch: BatchPatch = {};
    if (creditsOn) patch.credits = creditsValue;
    if (deadlineOn) patch.deadline = deadline.trim();
    if (channelOn) patch.channel = channel;
    onSubmit(patch, publish);
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>批量修改 {count} 个模型</DialogTitle>
          <DialogDescription>
            只改勾选的字段，其余保持不变。修改保存为新草稿，不影响线上版本。
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <label className="flex items-center gap-2 text-sm font-medium">
              <Checkbox checked={creditsOn} onCheckedChange={(v) => setCreditsOn(!!v)} />
              积分价格
            </label>
            {creditsOn && (
              <FormField label="统一设置为（积分）" error={creditsError ?? undefined}>
                <Input
                  inputMode="decimal"
                  value={credits}
                  onChange={(event) => setCredits(event.target.value)}
                  aria-label="积分价格"
                />
              </FormField>
            )}
          </div>
          <div className="flex flex-col gap-2">
            <label className="flex items-center gap-2 text-sm font-medium">
              <Checkbox checked={deadlineOn} onCheckedChange={(v) => setDeadlineOn(!!v)} />
              任务时限
            </label>
            {deadlineOn && (
              <FormField
                label="统一设置为"
                hint="示例：90s、30m、1h"
                error={deadlineError ?? undefined}
              >
                <Input
                  value={deadline}
                  onChange={(event) => setDeadline(event.target.value)}
                  aria-label="任务时限"
                />
              </FormField>
            )}
          </div>
          <div className="flex flex-col gap-2">
            <label className="flex items-center gap-2 text-sm font-medium">
              <Checkbox checked={channelOn} onCheckedChange={(v) => setChannelOn(!!v)} />
              所属渠道
            </label>
            {channelOn && (
              <FormField
                label="统一切换到"
                hint="上游模型 ID 不变；渠道不支持该类型时发布会被拒绝"
                error={channelError ?? undefined}
              >
                <NativeSelect
                  value={channel}
                  onChange={(event) => setChannel(event.target.value)}
                  aria-label="所属渠道"
                >
                  <option value="">选择渠道</option>
                  {channels.map((item) => (
                    <option key={item.key} value={item.key}>
                      {item.name}
                    </option>
                  ))}
                </NativeSelect>
              </FormField>
            )}
          </div>
          <label className="flex items-center gap-2 border-t pt-3 text-sm">
            <Checkbox checked={publish} onCheckedChange={(v) => setPublish(!!v)} />
            保存后立即发布（直接影响线上）
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={invalid} onClick={submit}>
            应用到 {count} 个模型
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
