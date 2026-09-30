import { useMemo, useState } from "react";
import { Loader2 } from "lucide-react";

import { importFromChannel } from "@/api/admin-ai";
import type { ChannelView, ModelDraft, PluginView } from "@/api/admin-ai/type";
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
import { channelMeta } from "@/utils/admin/plugin";
import { isRunnerDown } from "@/utils/admin/errors";
import { stashDrafts } from "@/utils/admin/import-draft";
import { draftToModelBody, normalizeDraft } from "@/utils/admin/model-body";
import {
  initialSettingValues,
  settingFields,
  validateSettingValues,
  type SettingFormValue,
  type SettingFormValues,
} from "@/utils/admin/settings-form";

import { SettingFields } from "../setting-fields";
import { Notice, Tag } from "../shared";
import { useAliveRef } from "../use-admin";

/**
 * 从渠道导入模型（admin 与 super_admin 都可用）：
 * 1. 参数：按插件 meta.import.args 渲染，没有参数就直接拉取；
 * 2. 草稿列表：勾选，可全选；
 * 3. 带入编辑器：草稿经 sessionStorage 传给模型页（键放 URL，内容不放），只是草稿，不会上架。
 * @param onImported 暂存成功（或用户选择不预填继续）后跳转到模型页；draftId 为 null 表示不预填
 */
export function ImportDialog({
  channel,
  plugins,
  onClose,
  onImported,
}: {
  channel: ChannelView | null;
  plugins: PluginView[];
  onClose: () => void;
  onImported: (draftId: string | null) => void;
}) {
  return (
    <Dialog open={!!channel} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-xl">
        {channel && (
          <ImportBody
            key={channel.key}
            channel={channel}
            plugins={plugins}
            onClose={onClose}
            onImported={onImported}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ImportBody({
  channel,
  plugins,
  onClose,
  onImported,
}: {
  channel: ChannelView;
  plugins: PluginView[];
  onClose: () => void;
  onImported: (draftId: string | null) => void;
}) {
  const aliveRef = useAliveRef();
  const meta = channelMeta(plugins, channel);
  const fields = useMemo(() => settingFields(meta?.import?.args), [meta]);
  const [values, setValues] = useState<SettingFormValues>(() => initialSettingValues(fields, null));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [runnerDown, setRunnerDown] = useState(false);
  const [drafts, setDrafts] = useState<ModelDraft[] | null>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [stashFailed, setStashFailed] = useState(false);

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
      setDrafts(list);
      setSelected(new Set());
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

  const allChecked = !!drafts && drafts.length > 0 && selected.size === drafts.length;

  const bringIn = () => {
    if (!drafts) return;
    const bodies = [...selected]
      .sort((a, b) => a - b)
      .map((index) => draftToModelBody(drafts[index], channel.key));
    const id = stashDrafts({ channelKey: channel.key, bodies });
    if (id === null) {
      setStashFailed(true);
      return;
    }
    onImported(id);
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>从渠道导入模型</DialogTitle>
        <DialogDescription>
          渠道 <b>{channel.name}</b>。导入的只是草稿，<b>不会上架</b>
          ，需要在模型页确认和试跑后发布。
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
            disabled={busy}
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

      {drafts && (
        <section aria-label="草稿列表" className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <h3 className="text-muted-foreground text-xs font-medium">
              {fields.length > 0 ? "2. " : "1. "}选择草稿
            </h3>
            {drafts.length > 0 && (
              <label className="ml-auto flex items-center gap-1.5 text-xs">
                <Checkbox
                  checked={allChecked}
                  onCheckedChange={(checked) =>
                    setSelected(
                      checked === true ? new Set(drafts.map((_, index) => index)) : new Set(),
                    )
                  }
                />
                全选
              </label>
            )}
          </div>
          {drafts.length === 0 ? (
            <p className="text-muted-foreground text-sm">这个渠道没有可导入的模型。</p>
          ) : (
            <ul className="flex max-h-64 flex-col gap-1.5 overflow-y-auto">
              {drafts.map((draft, index) => (
                <li key={`${draft.upstream_model}-${index}`}>
                  <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border px-2.5 py-2 text-sm">
                    <Checkbox
                      checked={selected.has(index)}
                      onCheckedChange={(checked) => toggle(index, checked === true)}
                      aria-label={`选择 ${draft.upstream_model}`}
                    />
                    <span className="font-mono text-xs">{draft.upstream_model}</span>
                    {draft.kind && <Tag>{draft.kind}</Tag>}
                    <span className="text-muted-foreground truncate text-xs">{draft.label}</span>
                  </label>
                </li>
              ))}
            </ul>
          )}
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
          可能是隐私模式。草稿无法带入编辑器，进入模型页后需要手动填写。
        </Notice>
      )}

      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          取消
        </Button>
        <Button
          variant={drafts ? "outline" : "default"}
          disabled={busy}
          onClick={() => void fetchDrafts()}
        >
          {busy && <Loader2 className="animate-spin" />}
          {drafts ? "重新拉取" : "拉取"}
        </Button>
        {drafts && (
          <Button disabled={selected.size === 0} onClick={bringIn}>
            带入编辑器（{selected.size}）
          </Button>
        )}
      </DialogFooter>
    </>
  );
}
