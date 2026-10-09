import { useState } from "react";
import { ArrowDownToLine, ArrowUpFromLine, Loader2, PencilLine, Trash2 } from "lucide-react";

import type { ChannelView } from "@/api/admin/ai/type.d";
import { Button } from "@/components/ui/button";
import {
  BulkActionButton,
  DataTableBulkActions,
} from "@/components/admin-ui/data-table-bulk-actions";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { useRetained } from "@/hooks/use-retained";
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
 * 勾选后出现的批量操作条：批量上线 / 下线 / 修改 / 删除。
 * 删除先二次确认；每个操作完成后弹出逐项结果。
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
  // 关闭结果框时 result 置空，内容留到退出动画播完
  const result = useRetained(batch.result);
  const showBar = selected.length > 0 || busy;

  const requestRemove = () =>
    void confirm({
      title: `删除 ${selected.length} 个模型？`,
      destructive: true,
      confirmLabel: "删除",
      description:
        "还在上线的会跳过（要先下线）。模型彻底删除，不能恢复；历史生成任务照常可以查看。",
      onConfirm: () => void batch.remove(selected),
    });

  return (
    <>
      {showBar && (
        <DataTableBulkActions count={selected.length} entityName="模型" onClear={onClear}>
          <BulkActionButton
            label="批量上线"
            icon={<ArrowUpFromLine />}
            disabled={busy}
            onClick={() => void batch.setEnabled(selected, true)}
          />
          <BulkActionButton
            label="批量下线"
            icon={<ArrowDownToLine />}
            disabled={busy}
            onClick={() => void batch.setEnabled(selected, false)}
          />
          <BulkActionButton
            label="批量修改"
            variant="default"
            icon={<PencilLine />}
            disabled={busy}
            onClick={() => setEditing(true)}
          />
          <BulkActionButton
            label="批量删除"
            icon={<Trash2 />}
            disabled={busy}
            onClick={requestRemove}
          />
        </DataTableBulkActions>
      )}

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
        onSubmit={(patch) => {
          setEditing(false);
          void batch.edit(selected, patch);
        }}
      />

      <Dialog open={!!batch.result && !busy} onOpenChange={(open) => !open && batch.clearResult()}>
        <DialogContent className="sm:max-w-lg">
          {result && (
            <>
              <DialogHeader>
                <DialogTitle>{batchLabel(result.action)}完成</DialogTitle>
                <DialogDescription>
                  成功 {result.done.length} 项，跳过 {result.skipped.length} 项，失败{" "}
                  {result.failed.length} 项。
                </DialogDescription>
              </DialogHeader>
              <div className="flex max-h-72 flex-col gap-2 overflow-y-auto">
                {result.failed.length > 0 && (
                  <Notice tone="danger" title="失败">
                    <ul className="mt-1 space-y-0.5">
                      {result.failed.map((item) => (
                        <li key={item.key}>
                          <code className="font-mono">{item.key}</code>：{item.reason}
                        </li>
                      ))}
                    </ul>
                  </Notice>
                )}
                {result.skipped.length > 0 && (
                  <Notice tone="warning" title="跳过">
                    <ul className="mt-1 space-y-0.5">
                      {result.skipped.map((item) => (
                        <li key={item.key}>
                          <code className="font-mono">{item.key}</code>：{item.reason}
                        </li>
                      ))}
                    </ul>
                  </Notice>
                )}
                {result.done.length > 0 && (
                  <Notice tone="success" title="成功">
                    <span className="font-mono">{result.done.join("、")}</span>
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
  onSubmit: (patch: BatchPatch) => void;
}) {
  const [creditsOn, setCreditsOn] = useState(false);
  const [credits, setCredits] = useState("");
  const [deadlineOn, setDeadlineOn] = useState(false);
  const [deadline, setDeadline] = useState("30m");
  const [channelOn, setChannelOn] = useState(false);
  const [channel, setChannel] = useState("");

  const creditsValue = Number(credits);
  const creditsError =
    creditsOn && (credits.trim() === "" || !Number.isInteger(creditsValue) || creditsValue <= 0)
      ? "请填写大于 0 的整数"
      : null;
  const deadlineError = deadlineOn ? checkDeadline(deadline) : null;
  const channelError = channelOn && !channel ? "请选择渠道" : null;
  const nothing = !creditsOn && !deadlineOn && !channelOn;
  const invalid = nothing || !!creditsError || !!deadlineError || !!channelError;

  const submit = () => {
    const patch: BatchPatch = {};
    if (creditsOn) patch.price = creditsValue;
    if (deadlineOn) patch.deadline = deadline.trim();
    if (channelOn) patch.channel = channel;
    onSubmit(patch);
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>批量修改 {count} 个模型</DialogTitle>
          <DialogDescription>
            只改勾选的字段，其余保持不变。已上线的模型保存后立即生效。
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <label className="flex items-center gap-2 text-sm font-medium">
              <Checkbox checked={creditsOn} onCheckedChange={(v) => setCreditsOn(!!v)} />
              默认价格
            </label>
            {creditsOn && (
              <FormField
                label="统一设置为（积分，按次是每次、按秒是每秒）"
                hint="只改默认价，规格价格不动；按 Token 计费的模型会跳过。"
                error={creditsError ?? undefined}
              >
                <Input
                  inputMode="numeric"
                  value={credits}
                  onChange={(event) => setCredits(event.target.value)}
                  aria-label="默认价格"
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
                hint="上游模型 ID 不变；渠道不支持该类型时，已上线的模型会保存失败"
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
