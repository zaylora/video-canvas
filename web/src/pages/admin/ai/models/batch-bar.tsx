import { useState } from "react";
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Blocks,
  Loader2,
  PencilLine,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";

import type { ChannelOption } from "@/api/admin/ai/type.d";
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
import { settingsProblem, switchModelKind, type ModelProtocol } from "@/utils/admin/model-protocol";
import { findPluginVersion } from "@/utils/admin/plugin";

import { FormField } from "@/components/admin-ui/form-field";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { Notice } from "@/components/admin-ui/notice";
import { ProtocolPicker } from "./protocol-picker";
import { batchLabel, type BatchPatch, type ModelBatch } from "./use-model-batch";
import type { ModelCatalog } from "./use-model-workspace";

/**
 * 勾选后出现的批量操作条：批量上线 / 下线 / 修改 / 设置能力与协议 / 删除，作用范围是勾选的（当前渠道当前页的）模型。
 * 删除先二次确认；每个操作完成后弹出逐项结果。
 */
export function BatchBar({
  selected,
  catalog,
  batch,
  onClear,
}: {
  selected: string[];
  /** 全部渠道（批量迁移渠道用）与插件（批量设置协议用） */
  catalog: ModelCatalog;
  batch: ModelBatch;
  onClear: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [settingProtocol, setSettingProtocol] = useState(false);
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
            label="批量设置能力与协议"
            icon={<Blocks />}
            disabled={busy}
            onClick={() => setSettingProtocol(true)}
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
        channels={catalog.channels}
        onClose={() => setEditing(false)}
        onSubmit={(patch) => {
          setEditing(false);
          void batch.edit(selected, patch);
        }}
      />

      <ProtocolDialog
        open={settingProtocol}
        count={selected.length}
        catalog={catalog}
        onClose={() => setSettingProtocol(false)}
        onSubmit={(kind, protocol) => {
          setSettingProtocol(false);
          void batch.setProtocol(selected, kind, protocol);
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
  channels: ChannelOption[];
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
                hint="上游模型 ID 与协议不变；已上线的模型换渠道后，新渠道要已启用且设置了 Key，否则保存失败"
                error={channelError ?? undefined}
              >
                <OptionSelect
                  value={channel}
                  onValueChange={setChannel}
                  aria-label="所属渠道"
                  options={[
                    { value: "", label: "选择渠道" },
                    ...channels.map((item) => ({ value: item.key, label: item.name })),
                  ]}
                />
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

/**
 * 批量设置能力与协议：复用模型弹窗里的协议选择器，应用到所选模型。
 * 只改能力与协议，不改价格；已启用的模型新配置过不了启用检查时，后端拒绝，结果里列出原因。
 */
function ProtocolDialog({
  open,
  count,
  catalog,
  onClose,
  onSubmit,
}: {
  open: boolean;
  count: number;
  catalog: ModelCatalog;
  onClose: () => void;
  onSubmit: (kind: string, protocol: ModelProtocol | null) => void;
}) {
  const { plugins, pluginsStatus } = catalog;
  const [kind, setKind] = useState("");
  const [protocol, setProtocol] = useState<ModelProtocol | null>(null);

  const changeKind = (next: string) => {
    const result = switchModelKind(plugins, protocol, next);
    setKind(next);
    setProtocol(result.protocol);
    if (result.notice) toast.info(result.notice);
  };
  const meta = protocol
    ? findPluginVersion(plugins, protocol.plugin, { version: protocol.version })?.meta
    : undefined;
  const problem = !kind
    ? "请先选择模型能力"
    : kind === "agent"
      ? null
      : !protocol
        ? "请选择调用协议"
        : meta
          ? settingsProblem(meta.channelSettings, protocol.settings)
          : "协议的版本不存在";
  const close = () => {
    setKind("");
    setProtocol(null);
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && close()}>
      <DialogContent className="max-h-[92svh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>批量设置能力与协议 · 已选 {count} 个模型</DialogTitle>
          <DialogDescription>
            会覆盖所选模型的能力、协议和协议设置，不改价格。已启用的模型如果新配置没有通过启用检查，会被拒绝并列出原因。
          </DialogDescription>
        </DialogHeader>
        <ProtocolPicker
          idPrefix="batch"
          kind={kind}
          protocol={protocol}
          plugins={plugins}
          pluginsStatus={pluginsStatus}
          onKindChange={changeKind}
          onProtocolChange={setProtocol}
        />
        <DialogFooter>
          <Button variant="outline" onClick={close}>
            取消
          </Button>
          <Button
            disabled={!!problem}
            title={problem ?? undefined}
            onClick={() => {
              onSubmit(kind, kind === "agent" ? null : protocol);
              setKind("");
              setProtocol(null);
            }}
          >
            应用到 {count} 个模型
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
