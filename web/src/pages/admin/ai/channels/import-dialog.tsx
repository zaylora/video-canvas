import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { Loader2 } from "lucide-react";

import {
  createModelDraft,
  importFromChannel,
  listModels,
  publishModel,
  setModelEnabled,
} from "@/api/admin/ai";
import type { ChannelView, ModelDraft, PluginView } from "@/api/admin/ai/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { type DialogControl } from "@/store/dialog";
import { errorMessage, isRunnerDown } from "@/utils/admin/errors";
import {
  buildImportBody,
  checkImportKey,
  initialImportRow,
  parseImportPrice,
  runImportJobs,
  savedKeys,
  summarizeImport,
  supportedKinds,
  type ImportMode,
  type ImportOutcome,
  type ImportRowFields,
  type KindGuess,
} from "@/utils/admin/import-batch";
import { stashDrafts } from "@/utils/admin/import-draft";
import { MODEL_KIND_LABEL, normalizeDraft } from "@/utils/admin/model-body";
import { publishBlockReason, resolveModelChannel } from "@/utils/admin/model-channel";
import { defaultCapabilities } from "@/utils/admin/model-template";
import { ignoredHints } from "@/utils/admin/param-hints";
import { channelMeta } from "@/utils/admin/plugin";
import {
  initialSettingValues,
  settingFields,
  validateSettingValues,
  type SettingFormValue,
  type SettingFormValues,
} from "@/utils/admin/settings-form";

import { SettingFields } from "../setting-fields";
import { useAliveRef } from "../../use-admin";

/**
 * 从渠道导入模型（admin 与 super_admin 都可用）：
 * 1. 参数：按插件 meta.import.args 渲染，没有参数就直接拉取；
 * 2. 批量处理表格：勾选、就地改展示名 / 能力类型 / 产品标识，上方统一设置默认价格；
 * 3. 处理方式：「全部保存为草稿」「全部上线」在对话框里逐个处理并给出结果；
 *    「逐个编辑」沿用旧流程，草稿经 sessionStorage 带到模型页逐个处理。
 * 执行中不能关闭对话框。走全局弹窗 store：openDialog(ImportDialog, { channel, plugins, onImported })。
 * @param onImported 「逐个编辑」暂存成功（或用户选择不预填继续）后跳转到模型页；draftId 为 null 表示不预填
 * @param onSaved 批量保存 / 上线后有模型入库时（刷新渠道页的模型清单）
 */
export function ImportDialog({
  channel,
  plugins,
  onImported,
  onSaved,
  open,
  onClose,
  onExited,
}: {
  channel: ChannelView;
  plugins: PluginView[];
  onImported: (draftId: string | null) => void;
  onSaved?: () => void;
} & DialogControl) {
  const [running, setRunning] = useState<ImportMode | null>(null);
  const [wide, setWide] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !running && onClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent
        showCloseButton={!running}
        className={cn("max-h-[90vh] overflow-y-auto", wide ? "sm:max-w-4xl" : "sm:max-w-xl")}
      >
        <ImportBody
          channel={channel}
          plugins={plugins}
          onClose={onClose}
          onImported={onImported}
          onSaved={onSaved}
          running={running}
          onRunningChange={setRunning}
          onWideChange={setWide}
        />
      </DialogContent>
    </Dialog>
  );
}

/** 表格一行：草稿 + 可编辑字段 */
type Row = ImportRowFields & KindGuess & { draft: ModelDraft };

function ImportBody({
  channel,
  plugins,
  onClose,
  onImported,
  onSaved,
  running,
  onRunningChange,
  onWideChange,
}: {
  channel: ChannelView;
  plugins: PluginView[];
  onClose: () => void;
  onImported: (draftId: string | null) => void;
  onSaved?: () => void;
  /** 正在执行的批量方式；null 表示空闲 */
  running: ImportMode | null;
  onRunningChange: (running: ImportMode | null) => void;
  onWideChange: (wide: boolean) => void;
}) {
  const aliveRef = useAliveRef();
  const navigate = useNavigate();
  const meta = channelMeta(plugins, channel);
  const kinds = useMemo(() => supportedKinds(meta), [meta]);
  const fields = useMemo(() => settingFields(meta?.import?.args), [meta]);
  const [values, setValues] = useState<SettingFormValues>(() => initialSettingValues(fields, null));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [runnerDown, setRunnerDown] = useState(false);
  const [rows, setRows] = useState<Row[] | null>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  /** 已经落库的行（本次对话框里处理过的），不能再勾选 */
  const [handled, setHandled] = useState<Set<number>>(new Set());
  const [priceText, setPriceText] = useState("");
  const [stashFailed, setStashFailed] = useState(false);
  /** 已存在的模型 key；null 表示还没取到（取不到时冲突交给后端兜底） */
  const [existing, setExisting] = useState<Set<string> | null>(null);
  const [existingFailed, setExistingFailed] = useState(false);
  const [progress, setProgress] = useState({ done: 0, total: 0 });
  const [outcomes, setOutcomes] = useState<ImportOutcome[] | null>(null);
  /** 「全部上线」的就地二次确认（对话框里不再叠一层确认框，避免 Esc 把外层一起关掉） */
  const [confirmOnline, setConfirmOnline] = useState(false);

  useEffect(() => {
    onWideChange(rows !== null && rows.length > 0);
  }, [rows, onWideChange]);

  useEffect(() => {
    let cancelled = false;
    listModels()
      .then((list) => !cancelled && setExisting(new Set(list.map((item) => item.key))))
      .catch(() => !cancelled && setExistingFailed(true));
    return () => {
      cancelled = true;
    };
  }, []);

  /** 渠道本身上不了线的原因（停用、需要 Key 却没设置）；能上线为 null */
  const onlineBlock = useMemo(
    () =>
      publishBlockReason(
        resolveModelChannel({ channels: [{ channel: channel.key }] }, [channel], plugins),
        true,
      ),
    [channel, plugins],
  );

  const fetchDrafts = async () => {
    const checked = validateSettingValues(fields, values);
    if (!checked.ok) {
      setErrors(checked.errors);
      return;
    }
    setErrors({});
    setBusy(true);
    setRunnerDown(false);
    try {
      const result = await importFromChannel(channel.key, checked.values);
      if (!aliveRef.current) return;
      const list = result.drafts
        .map((raw) => normalizeDraft(raw))
        .filter((draft): draft is ModelDraft => draft !== null);
      setRows(list.map((draft) => ({ ...initialImportRow(draft, kinds), draft })));
      setSelected(new Set());
      setHandled(new Set());
      setOutcomes(null);
      setConfirmOnline(false);
    } catch (error) {
      // 全局 toast 已弹；runner 不可用时在对话框里说明并保留参数，方便重试
      if (aliveRef.current && isRunnerDown(error)) setRunnerDown(true);
    } finally {
      if (aliveRef.current) setBusy(false);
    }
  };

  const toggle = (index: number, checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(index);
      else next.delete(index);
      return next;
    });

  const patchRow = (index: number, patch: Partial<ImportRowFields>) =>
    setRows((prev) => prev && prev.map((row, i) => (i === index ? { ...row, ...patch } : row)));

  const selectable = useMemo(
    () => (rows ?? []).map((_, index) => index).filter((index) => !handled.has(index)),
    [rows, handled],
  );
  /** 勾选的行，按表格顺序 */
  const order = useMemo(() => [...selected].sort((a, b) => a - b), [selected]);
  const allChecked = selectable.length > 0 && selectable.every((index) => selected.has(index));

  /** 每行 key 的问题：只和“已存在的模型”与“本批其他勾选行”比 */
  const keyErrors = useMemo(() => {
    const out = new Map<number, string>();
    if (!rows) return out;
    const taken = existing ?? new Set<string>();
    rows.forEach((row, index) => {
      if (handled.has(index)) return;
      const others = order.filter((i) => i !== index).map((i) => rows[i].key.trim());
      const error = checkImportKey(row.key, taken, selected.has(index) ? others : []);
      if (error) out.set(index, error);
    });
    return out;
  }, [rows, existing, handled, selected, order]);

  const price = parseImportPrice(priceText);
  const selectedErrors = order.filter((index) => keyErrors.has(index)).length;
  const blocked = !!running || order.length === 0 || selectedErrors > 0 || !price.ok;

  /** 勾选行 → 要保存的正文 */
  const buildJobs = () =>
    order.map((index) => {
      const row = (rows as Row[])[index];
      const built = buildImportBody(
        row.draft,
        channel.key,
        row,
        price.ok ? price.value : undefined,
      );
      return {
        index,
        key: row.key.trim(),
        label: row.label.trim() || row.draft.upstream_model,
        ...built,
      };
    });

  const editOneByOne = () => {
    const id = stashDrafts({ channelKey: channel.key, bodies: buildJobs().map((job) => job.body) });
    if (id === null) {
      setStashFailed(true);
      return;
    }
    onImported(id);
  };

  const runBatch = async (mode: ImportMode) => {
    const jobs = buildJobs();
    setConfirmOnline(false);
    setOutcomes(null);
    setProgress({ done: 0, total: jobs.length });
    onRunningChange(mode);
    try {
      const out = await runImportJobs(
        jobs,
        mode,
        {
          createDraft: (body) => createModelDraft(body, "从渠道导入"),
          publish: publishModel,
          setEnabled: setModelEnabled,
        },
        (done, total) => aliveRef.current && setProgress({ done, total }),
        (error) => errorMessage(error, "失败"),
      );
      if (!aliveRef.current) return;
      const saved = new Set(savedKeys(out));
      if (saved.size > 0) onSaved?.();
      setExisting((prev) => new Set([...(prev ?? []), ...saved]));
      setHandled((prev) => {
        const next = new Set(prev);
        for (const job of jobs) if (saved.has(job.key)) next.add(job.index);
        return next;
      });
      setSelected((prev) => {
        const next = new Set(prev);
        for (const job of jobs) if (saved.has(job.key)) next.delete(job.index);
        return next;
      });
      setOutcomes(out);
    } finally {
      onRunningChange(null);
    }
  };

  const summary = outcomes ? summarizeImport(outcomes) : null;
  const step = (n: number) => `${fields.length > 0 ? n + 1 : n}. `;

  return (
    <>
      <DialogHeader>
        <DialogTitle>从渠道导入模型</DialogTitle>
        <DialogDescription>
          渠道 <b>{channel.name}</b>
          。勾选要导入的模型，可以直接保存为草稿或一键上线；需要细调的选「逐个编辑」。
        </DialogDescription>
      </DialogHeader>

      {fields.length > 0 && (
        <section aria-label="导入参数" className="flex flex-col gap-2">
          <h3 className="text-muted-foreground text-xs font-medium">1. 参数</h3>
          <SettingFields
            idPrefix="import-arg"
            fields={fields}
            values={values}
            errors={errors}
            disabled={busy || !!running}
            onChange={(name: string, value: SettingFormValue) =>
              setValues((prev) => ({ ...prev, [name]: value }))
            }
          />
        </section>
      )}

      {runnerDown && (
        <Notice tone="danger" title="插件运行器暂时不可用">
          参数已保留，稍后重试。
        </Notice>
      )}

      {rows && rows.length === 0 && (
        <p className="text-muted-foreground text-sm">这个渠道没有可导入的模型。</p>
      )}

      {rows && rows.length > 0 && (
        <>
          <section aria-label="统一设置" className="flex flex-col gap-2">
            <h3 className="text-muted-foreground text-xs font-medium">{step(1)}统一设置</h3>
            <FormField
              label="默认价格（积分）"
              htmlFor="import-price"
              error={price.ok ? undefined : price.message}
              hint="留空不改，用各类型的默认价；按次计费改每次价格，按秒计费改每秒价格，按 Token 计费的不改。"
              className="max-w-xs"
            >
              <Input
                id="import-price"
                inputMode="numeric"
                placeholder="留空不改"
                value={priceText}
                disabled={!!running}
                aria-invalid={!price.ok}
                onChange={(event) => setPriceText(event.target.value)}
              />
            </FormField>
          </section>

          <section aria-label="待导入模型" className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <h3 className="text-muted-foreground text-xs font-medium">
                {step(2)}选择模型（已选 {order.length} / {selectable.length}）
              </h3>
              {existingFailed && (
                <span className="text-muted-foreground text-xs">
                  没取到已有模型列表，重名会在保存时提示
                </span>
              )}
            </div>
            <div className="max-h-80 overflow-y-auto rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-8">
                      <Checkbox
                        aria-label="全选"
                        checked={allChecked}
                        disabled={!!running || selectable.length === 0}
                        onCheckedChange={(checked) =>
                          setSelected(checked === true ? new Set(selectable) : new Set())
                        }
                      />
                    </TableHead>
                    <TableHead>展示名</TableHead>
                    <TableHead>上游模型 ID</TableHead>
                    <TableHead className="w-28">能力类型</TableHead>
                    <TableHead>产品标识</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((row, index) => (
                    <ImportRow
                      key={`${row.draft.upstream_model}-${index}`}
                      row={row}
                      kinds={kinds}
                      checked={selected.has(index)}
                      handled={handled.has(index)}
                      disabled={!!running}
                      keyError={keyErrors.get(index)}
                      onCheck={(checked) => toggle(index, checked)}
                      onChange={(patch) => patchRow(index, patch)}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                size="xs"
                variant="outline"
                disabled={!!running || selectable.length === 0}
                onClick={() =>
                  setSelected((prev) => new Set(selectable.filter((index) => !prev.has(index))))
                }
              >
                反选
              </Button>
              {selectedErrors > 0 && (
                <span className="text-destructive self-center text-xs">
                  已选的模型里有 {selectedErrors} 个产品标识需要修改
                </span>
              )}
            </div>
          </section>
        </>
      )}

      {running && (
        <Notice tone="info" title={`正在处理 ${progress.done} / ${progress.total}`}>
          逐个保存中，请不要关闭对话框。
        </Notice>
      )}

      {confirmOnline && !running && (
        <Notice
          tone="warning"
          title={`上线 ${order.length} 个模型？`}
          action={
            <div className="flex gap-1.5">
              <Button size="xs" variant="outline" onClick={() => setConfirmOnline(false)}>
                再想想
              </Button>
              <Button size="xs" onClick={() => void runBatch("online")}>
                确认上线
              </Button>
            </div>
          }
        >
          上线后用户马上能用；有问题的只存为草稿，不会上线。
        </Notice>
      )}

      {outcomes && summary && (
        <section aria-label="处理结果" className="flex flex-col gap-2">
          <Notice
            tone={summary.failed > 0 ? "warning" : "success"}
            title={`处理完成：成功 ${summary.done} 个，跳过 ${summary.skipped} 个，失败 ${summary.failed} 个`}
            action={
              <Button size="xs" variant="outline" onClick={() => navigate("/admin/ai/models")}>
                去模型列表看看
              </Button>
            }
          />
          <ul className="flex max-h-48 flex-col gap-1 overflow-y-auto text-sm">
            {outcomes.map((item) => (
              <li key={item.key} className="flex items-baseline gap-2">
                <Tag
                  tone={
                    item.status === "done"
                      ? "success"
                      : item.status === "skipped"
                        ? "warning"
                        : "danger"
                  }
                >
                  {item.status === "done" ? "成功" : item.status === "skipped" ? "跳过" : "失败"}
                </Tag>
                <span className="truncate">{item.label}</span>
                <span className="text-muted-foreground font-mono text-xs">{item.key}</span>
                {item.reason && (
                  <span className="text-muted-foreground min-w-0 text-xs">{item.reason}</span>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      {stashFailed && (
        <Notice
          tone="warning"
          title="浏览器不允许暂存草稿"
          action={
            <Button size="xs" variant="outline" onClick={() => onImported(null)}>
              仍然进入模型页
            </Button>
          }
        >
          可能是隐私模式。草稿无法带入编辑器，可以改用「全部保存为草稿」，或进入模型页后手动填写。
        </Notice>
      )}

      {rows && rows.length > 0 && onlineBlock && (
        <p className="text-muted-foreground text-xs">暂时不能上线：{onlineBlock}</p>
      )}

      <DialogFooter className="flex-wrap">
        <Button variant="outline" disabled={!!running} onClick={onClose}>
          {outcomes ? "关闭" : "取消"}
        </Button>
        <Button
          variant={rows ? "outline" : "default"}
          disabled={busy || !!running}
          onClick={() => void fetchDrafts()}
        >
          {busy && <Loader2 className="animate-spin" />}
          {rows ? "重新拉取" : "拉取"}
        </Button>
        {rows && rows.length > 0 && (
          <>
            <Button variant="outline" disabled={blocked} onClick={editOneByOne}>
              逐个编辑
            </Button>
            <Button variant="outline" disabled={blocked} onClick={() => void runBatch("draft")}>
              {running === "draft" && <Loader2 className="animate-spin" />}
              全部保存为草稿（{order.length}）
            </Button>
            <Button disabled={blocked || !!onlineBlock} onClick={() => setConfirmOnline(true)}>
              {running === "online" && <Loader2 className="animate-spin" />}
              全部上线（{order.length}）
            </Button>
          </>
        )}
      </DialogFooter>
    </>
  );
}

/** 表格一行：勾选、展示名、上游模型 ID、能力类型、产品标识 */
function ImportRow({
  row,
  kinds,
  checked,
  handled,
  disabled,
  keyError,
  onCheck,
  onChange,
}: {
  row: Row;
  kinds: readonly string[];
  checked: boolean;
  /** 已在本次对话框里保存过 */
  handled: boolean;
  disabled: boolean;
  keyError?: string;
  onCheck: (checked: boolean) => void;
  onChange: (patch: Partial<ImportRowFields>) => void;
}) {
  const upstream = row.draft.upstream_model;
  const locked = disabled || handled;
  const ignored = ignoredHints(defaultCapabilities(row.kind), row.draft.param_hints);
  const kindOptions = kinds.length > 0 ? kinds : Object.keys(MODEL_KIND_LABEL);
  return (
    <TableRow data-state={checked ? "selected" : undefined} className={cn(handled && "opacity-60")}>
      <TableCell className="align-top">
        <Checkbox
          aria-label={`选择 ${upstream}`}
          checked={checked}
          disabled={locked}
          onCheckedChange={(next) => onCheck(next === true)}
        />
      </TableCell>
      <TableCell className="align-top">
        <Input
          aria-label={`${upstream} 的展示名`}
          className="h-8 min-w-36"
          value={row.label}
          placeholder={upstream}
          disabled={locked}
          onChange={(event) => onChange({ label: event.target.value })}
        />
      </TableCell>
      <TableCell className="align-top">
        <div className="flex flex-col gap-1 pt-1.5">
          <span className="font-mono text-xs break-all">{upstream}</span>
          {handled && <Tag tone="success">已处理</Tag>}
          {ignored.length > 0 && (
            <Tag
              tone="warning"
              title="插件建议预填的这些参数不在默认模板里，已忽略；需要的话在模型页「能力与参数」里手动添加"
            >
              忽略了未知参数 {ignored.join("、")}
            </Tag>
          )}
        </div>
      </TableCell>
      <TableCell className="align-top">
        {row.certain ? (
          <Tag className="mt-1.5">{MODEL_KIND_LABEL[row.kind] ?? row.kind}</Tag>
        ) : (
          <NativeSelect
            aria-label={`${upstream} 的能力类型`}
            title="插件没给出能力类型，请确认"
            className="h-8 border-amber-500/60"
            value={row.kind}
            disabled={locked}
            onChange={(event) => onChange({ kind: event.target.value })}
          >
            {kindOptions.map((kind) => (
              <option key={kind} value={kind}>
                {MODEL_KIND_LABEL[kind] ?? kind}
              </option>
            ))}
          </NativeSelect>
        )}
      </TableCell>
      <TableCell className="align-top">
        <Input
          aria-label={`${upstream} 的产品标识`}
          className="h-8 min-w-40 font-mono text-xs"
          value={row.key}
          disabled={locked}
          aria-invalid={!handled && !!keyError}
          onChange={(event) => onChange({ key: event.target.value })}
        />
        {!handled && keyError && (
          <p className="text-destructive mt-1 text-xs" role="alert">
            {keyError}
          </p>
        )}
      </TableCell>
    </TableRow>
  );
}
