import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { KeyRound, Loader2, Stethoscope } from "lucide-react";

import { createChannel, getChannel, updateChannel } from "@/api/admin-ai";
import type { ChannelView, PluginView } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  buildChannelRequest,
  channelFormFromView,
  emptyChannelForm,
  rebaseSettings,
  type ChannelFormState,
} from "@/utils/admin/channel-form";
import { errorMessage, isChannelKeyExists } from "@/utils/admin/errors";
import {
  availableUpgrade,
  describeAuth,
  findPluginVersion,
  latestVersion,
  shortSha,
} from "@/utils/admin/plugin";
import { settingFields, type SettingFormValue } from "@/utils/admin/settings-form";
import { formatTime } from "@/utils/time";

import { SettingFields } from "../setting-fields";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { ReadOnlyNotice } from "../shared";
import { useAliveRef } from "../use-admin";
import { CheckResult } from "./check-result";
import { SecretDialog } from "./secret-dialog";
import type { CheckState } from "./use-channel-check";

/** 抽屉打开的对象：新建（可预选插件），或已有渠道 */
export type ChannelSheetTarget =
  | { kind: "new"; pluginKey?: string }
  | { kind: "edit"; channel: ChannelView };

/** 一个分区：标题 + 内容 */
function Zone({ title, risk, children }: { title: string; risk?: boolean; children: ReactNode }) {
  return (
    <section
      className={`flex flex-col gap-3 rounded-lg border p-3.5 ${risk ? "border-amber-500/60" : ""}`}
    >
      <h3 className="text-sm font-medium">{title}</h3>
      {children}
    </section>
  );
}

/** 危险开关一行：标题 + 字段名 + 后果说明 + 开关 */
function RiskRow({
  id,
  title,
  field,
  description,
  checked,
  disabled,
  onChange,
}: {
  id: string;
  title: string;
  field: string;
  description: ReactNode;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start gap-3">
      <div className="min-w-0 flex-1">
        <Label htmlFor={id} className="text-sm">
          {title}{" "}
          <span className="text-muted-foreground font-mono text-xs font-normal">{field}</span>
        </Label>
        <p className="text-muted-foreground mt-0.5 text-xs">{description}</p>
      </div>
      <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onChange} />
    </div>
  );
}

/**
 * 渠道抽屉：新建 / 编辑；admin 打开为只读（操作区不渲染）。
 * 五个区：基本、插件、连接（含插件设置）、限流、高级与安全，Key 单独一区。
 * 两个危险开关旁直接写后果说明，从关到开要勾选确认；allow_credentials 仅所选版本 auth=custom 可用。
 * @param pluginsReady 插件清单已加载（未就绪时抽屉里显示骨架，避免版本下拉是空的）
 * @param onSaved 新建 / 更新 / 设 Key 后，通知列表刷新
 */
export function ChannelSheet({
  target,
  plugins,
  pluginsReady,
  canWrite,
  checks,
  onCheck,
  onSaved,
  onClose,
}: {
  target: ChannelSheetTarget | null;
  plugins: PluginView[];
  pluginsReady: boolean;
  canWrite: boolean;
  /** 各渠道的检查状态（按 key）；新建成功后抽屉转为编辑态，要按新 key 取 */
  checks: Record<string, CheckState>;
  onCheck: (key: string) => void;
  onSaved: () => void;
  onClose: () => void;
}) {
  /** 由 Body 上报“是否有未保存修改”，关闭时据此确认 */
  const [dirty, setDirty] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);

  const requestClose = () => {
    if (dirty) setConfirmClose(true);
    else onClose();
  };

  return (
    <>
      <Sheet open={!!target} onOpenChange={(next) => !next && requestClose()}>
        <SheetContent className="w-full data-[side=right]:sm:max-w-xl">
          {target && (
            <SheetBody
              key={target.kind === "edit" ? target.channel.key : `new:${target.pluginKey ?? ""}`}
              target={target}
              plugins={plugins}
              pluginsReady={pluginsReady}
              canWrite={canWrite}
              checks={checks}
              onCheck={onCheck}
              onSaved={onSaved}
              onDirtyChange={setDirty}
              onClose={requestClose}
            />
          )}
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={confirmClose}
        title="放弃未保存的修改？"
        description="关闭后，这次对渠道的修改不会保存。"
        confirmLabel="放弃修改"
        destructive
        onConfirm={() => {
          setConfirmClose(false);
          setDirty(false);
          onClose();
        }}
        onCancel={() => setConfirmClose(false)}
      />
    </>
  );
}

function SheetBody({
  target,
  plugins,
  pluginsReady,
  canWrite,
  checks,
  onCheck,
  onSaved,
  onDirtyChange,
  onClose,
}: {
  target: ChannelSheetTarget;
  plugins: PluginView[];
  pluginsReady: boolean;
  canWrite: boolean;
  checks: Record<string, CheckState>;
  onCheck: (key: string) => void;
  onSaved: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onClose: () => void;
}) {
  const aliveRef = useAliveRef();
  /** 已存在的渠道；新建成功后会变成刚创建的那个，抽屉转为编辑态 */
  const [original, setOriginal] = useState<ChannelView | null>(
    target.kind === "edit" ? target.channel : null,
  );
  const readOnly = !canWrite;

  const initialForm = useMemo<ChannelFormState | null>(() => {
    if (!pluginsReady) return null;
    if (target.kind === "edit") {
      const meta = findPluginVersion(plugins, target.channel.plugin_key, {
        id: target.channel.plugin_version_id,
        version: target.channel.plugin_version,
      })?.meta;
      return channelFormFromView(target.channel, settingFields(meta?.channelSettings));
    }
    const base = emptyChannelForm(plugins);
    const preferred = target.pluginKey
      ? plugins.find((plugin) => plugin.key === target.pluginKey)
      : undefined;
    if (!preferred) return base;
    const version = latestVersion(preferred)?.version ?? "";
    const meta = preferred.versions.find((item) => item.version === version)?.meta;
    return {
      ...base,
      pluginKey: preferred.key,
      pluginVersion: version,
      settings: rebaseSettings([], {}, settingFields(meta?.channelSettings)),
    };
    // 只在打开时算一次；之后的编辑都在 form 里
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pluginsReady]);

  if (!initialForm) {
    return (
      <>
        <SheetHeader>
          <SheetTitle>渠道</SheetTitle>
        </SheetHeader>
        <div className="flex flex-col gap-3 p-4" aria-busy="true">
          <Skeleton className="h-24" />
          <Skeleton className="h-40" />
        </div>
      </>
    );
  }

  return (
    <SheetForm
      aliveRef={aliveRef}
      initialForm={initialForm}
      original={original}
      setOriginal={setOriginal}
      plugins={plugins}
      readOnly={readOnly}
      checks={checks}
      onCheck={onCheck}
      onSaved={onSaved}
      onDirtyChange={onDirtyChange}
      onClose={onClose}
    />
  );
}

function SheetForm({
  aliveRef,
  initialForm,
  original,
  setOriginal,
  plugins,
  readOnly,
  checks,
  onCheck,
  onSaved,
  onDirtyChange,
  onClose,
}: {
  aliveRef: { current: boolean };
  initialForm: ChannelFormState;
  original: ChannelView | null;
  setOriginal: (channel: ChannelView) => void;
  plugins: PluginView[];
  readOnly: boolean;
  checks: Record<string, CheckState>;
  onCheck: (key: string) => void;
  onSaved: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onClose: () => void;
}) {
  const isNew = !original;
  const check = original ? checks[original.key] : undefined;
  const [form, setFormState] = useState<ChannelFormState>(initialForm);
  const [baseline, setBaseline] = useState(() => JSON.stringify(initialForm));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [dropped, setDropped] = useState<string[]>([]);
  const [credsReset, setCredsReset] = useState(false);
  const [justCreated, setJustCreated] = useState(false);
  const [justSetKey, setJustSetKey] = useState(false);
  const [risk, setRisk] = useState<"trusted" | "cred" | null>(null);
  const [riskChecked, setRiskChecked] = useState(false);
  const [secretOpen, setSecretOpen] = useState(false);

  /** 抽屉卸载（关闭、切换对象）时清掉脏标记，避免下次打开误提示“放弃修改” */
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const setForm = (next: ChannelFormState) => {
    setFormState(next);
    onDirtyChange(JSON.stringify(next) !== baseline);
  };
  const patch = (partial: Partial<ChannelFormState>) => setForm({ ...form, ...partial });

  const plugin = plugins.find((item) => item.key === form.pluginKey);
  const version = findPluginVersion(plugins, form.pluginKey, { version: form.pluginVersion });
  const meta = version?.meta ?? undefined;
  const fields = useMemo(() => settingFields(meta?.channelSettings), [meta]);
  const authType = meta?.auth?.type ?? "none";
  const isCustomAuth = authType === "custom";
  const upgrade = original
    ? availableUpgrade(plugins, { plugin_key: form.pluginKey, plugin_version: form.pluginVersion })
    : null;
  const switchedVersion = !!original && form.pluginVersion !== original.plugin_version;

  /** 换插件 / 版本：设置项用 rebaseSettings 迁移，被丢弃的项提示；allow_credentials 不再可用时自动关闭 */
  const changePluginVersion = (pluginKey: string, versionText: string) => {
    const nextVersion = findPluginVersion(plugins, pluginKey, { version: versionText });
    const nextFields = settingFields(nextVersion?.meta?.channelSettings);
    const nextSettings = rebaseSettings(fields, form.settings, nextFields);
    const gone = fields
      .filter((field) => {
        const value = form.settings[field.name];
        const had =
          typeof value === "boolean" ? value : typeof value === "string" && value.trim() !== "";
        return had && !nextFields.some((next) => next.name === field.name);
      })
      .map((field) => field.label);
    setDropped(gone);
    const nextCustom = nextVersion?.meta?.auth?.type === "custom";
    const reset = form.allowCredentials && !nextCustom;
    setCredsReset(reset);
    setForm({
      ...form,
      pluginKey,
      pluginVersion: versionText,
      settings: nextSettings,
      allowCredentials: reset ? false : form.allowCredentials,
    });
  };

  const onPluginChange = (key: string) => {
    const next = plugins.find((item) => item.key === key);
    changePluginVersion(key, latestVersion(next)?.version ?? "");
  };

  const openRisk = (which: "trusted" | "cred") => {
    setRiskChecked(false);
    setRisk(which);
  };

  const confirmRisk = () => {
    if (risk === "trusted") patch({ trustedInternal: true });
    if (risk === "cred") patch({ allowCredentials: true });
    setRisk(null);
  };

  const save = async () => {
    const built = buildChannelRequest(form, fields, original);
    if (!built.ok) {
      setErrors(built.errors);
      setFormError(null);
      return;
    }
    setErrors({});
    setFormError(null);
    setSaving(true);
    try {
      let saved: ChannelView | null = null;
      if (!original) {
        saved = await createChannel(built.create);
        setJustCreated(true);
      } else if (built.changed) {
        saved = await updateChannel(original.key, built.update);
        setJustCreated(false);
      }
      if (!aliveRef.current) return;
      if (saved) {
        setOriginal(saved);
        onSaved();
      }
      // 保存后的状态成为新的基线，脏状态清零
      setBaseline(JSON.stringify(form));
      onDirtyChange(false);
      setDropped([]);
      setCredsReset(false);
    } catch (error) {
      // 全局 toast 已弹；这里把原因就地放在表单里（50013 放顶部，50012 定位到 key 字段）
      if (!aliveRef.current) return;
      if (isChannelKeyExists(error)) setErrors({ key: errorMessage(error, "这个 key 已经存在") });
      else setFormError(errorMessage(error, "保存失败"));
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  const refreshAfterSecret = async () => {
    if (!original) return;
    setJustSetKey(true);
    try {
      const fresh = await getChannel(original.key);
      if (aliveRef.current) setOriginal(fresh);
    } finally {
      onSaved();
    }
  };

  const changeSetting = (name: string, value: SettingFormValue) =>
    setForm({ ...form, settings: { ...form.settings, [name]: value } });

  const title = isNew ? "新建渠道" : readOnly ? "查看渠道" : "编辑渠道";
  const pluginLabel = plugin ? `${plugin.name} v${form.pluginVersion}` : form.pluginKey;
  const checkOk = check && !check.busy && check.outcome.kind === "ok";

  return (
    <>
      <SheetHeader className="border-b">
        <SheetTitle>
          {title}
          {original && (
            <span className="text-muted-foreground ml-2 font-mono text-xs font-normal">
              {original.key}
            </span>
          )}
        </SheetTitle>
        <SheetDescription>渠道把一个插件版本、一个地址和一个 Key 绑在一起。</SheetDescription>
      </SheetHeader>

      <form
        id="channel-form"
        className="flex min-h-0 flex-1 flex-col gap-3.5 overflow-y-auto px-4 pb-4"
        onSubmit={(event) => {
          event.preventDefault();
          if (!readOnly) void save();
        }}
      >
        {readOnly && <ReadOnlyNotice what="修改渠道" />}
        {formError && (
          <Notice tone="danger" title="保存失败">
            {formError}
          </Notice>
        )}
        {justCreated && original && (
          <Notice tone="success" title="渠道已创建">
            下一步：在下面设置 Key，再检查连通性。
          </Notice>
        )}

        <Zone title="基本">
          <FormField
            label="标识 key"
            htmlFor="channel-key"
            error={errors.key}
            hint={isNew ? "小写字母、数字、连字符；创建后不可修改" : "不可修改"}
          >
            <Input
              id="channel-key"
              className="font-mono"
              value={form.key}
              placeholder="如 newapi-main"
              disabled={!isNew || readOnly}
              aria-invalid={!!errors.key}
              onChange={(event) => patch({ key: event.target.value })}
            />
          </FormField>
          <FormField label="名称" htmlFor="channel-name" error={errors.name}>
            <Input
              id="channel-name"
              value={form.name}
              disabled={readOnly}
              aria-invalid={!!errors.name}
              onChange={(event) => patch({ name: event.target.value })}
            />
          </FormField>
          <div className="flex items-center gap-2 text-sm">
            <Switch
              id="channel-enabled"
              checked={form.enabled}
              disabled={readOnly}
              onCheckedChange={(checked) => patch({ enabled: checked })}
            />
            <Label htmlFor="channel-enabled">启用</Label>
            <span className="text-muted-foreground text-xs">
              停用后不再接新任务，进行中的任务按快照继续
            </span>
          </div>
        </Zone>

        <Zone title="插件">
          <div className="grid grid-cols-2 gap-3">
            <FormField label="插件" htmlFor="channel-plugin" error={errors.plugin}>
              <NativeSelect
                id="channel-plugin"
                value={form.pluginKey}
                disabled={readOnly}
                onChange={(event) => onPluginChange(event.target.value)}
              >
                {!plugin && <option value={form.pluginKey}>{form.pluginKey || "请选择"}</option>}
                {plugins.map((item) => (
                  <option key={item.key} value={item.key}>
                    {item.name}
                    {item.enabled ? "" : "（已停用）"}
                  </option>
                ))}
              </NativeSelect>
            </FormField>
            <FormField label="固定版本" htmlFor="channel-version">
              <NativeSelect
                id="channel-version"
                value={form.pluginVersion}
                disabled={readOnly}
                onChange={(event) => changePluginVersion(form.pluginKey, event.target.value)}
              >
                {!version && (
                  <option value={form.pluginVersion}>{form.pluginVersion || "请选择"}</option>
                )}
                {(plugin?.versions ?? []).map((item) => (
                  <option key={item.id} value={item.version}>
                    {item.version}
                  </option>
                ))}
              </NativeSelect>
            </FormField>
          </div>
          {version && (
            <p className="text-muted-foreground text-xs">
              鉴权：{describeAuth(meta?.auth)} · sha256{" "}
              <span className="font-mono">{shortSha(version.sha256)}</span>
            </p>
          )}
          {upgrade && (
            <Notice tone="info" title={`有新版本 ${upgrade.version}`}>
              切换后新任务用新版本，进行中的任务按旧版本跑完。<b>切换后建议先对相关模型试跑。</b>
            </Notice>
          )}
          {switchedVersion && !upgrade && (
            <Notice tone="info">
              保存后新任务改用 v{form.pluginVersion}
              ，进行中的任务按旧版本跑完。建议保存后先对使用这个渠道的模型试跑。
            </Notice>
          )}
          {dropped.length > 0 && (
            <Notice tone="warning">新版本不再有这些设置项，已丢弃：{dropped.join("、")}。</Notice>
          )}
          {credsReset && (
            <Notice tone="warning">新版本不需要接触 Key，已自动关闭“允许插件读取 Key”。</Notice>
          )}
        </Zone>

        <Zone title="连接">
          <FormField
            label="base_url"
            htmlFor="channel-base-url"
            error={errors.baseUrl}
            hint="插件请求只能去这个地址；http/https，不能带用户名密码"
          >
            <Input
              id="channel-base-url"
              className="font-mono"
              value={form.baseUrl}
              placeholder="https://gw.example.com"
              disabled={readOnly}
              aria-invalid={!!errors.baseUrl}
              onChange={(event) => patch({ baseUrl: event.target.value })}
            />
          </FormField>
        </Zone>

        {fields.length > 0 && (
          <Zone title="插件设置">
            <SettingFields
              idPrefix="channel-setting"
              fields={fields}
              values={form.settings}
              errors={Object.fromEntries(
                Object.entries(errors)
                  .filter(([key]) => key.startsWith("settings."))
                  .map(([key, message]) => [key.slice("settings.".length), message]),
              )}
              disabled={readOnly}
              onChange={changeSetting}
            />
          </Zone>
        )}

        <Zone title="限流">
          <div className="grid grid-cols-2 gap-3">
            <FormField label="每秒请求数 rps" htmlFor="channel-rps" error={errors.rps}>
              <Input
                id="channel-rps"
                type="number"
                min={0}
                value={form.rps}
                disabled={readOnly}
                aria-invalid={!!errors.rps}
                onChange={(event) => patch({ rps: event.target.value })}
              />
            </FormField>
            <FormField label="最大并发" htmlFor="channel-concurrency" error={errors.maxConcurrency}>
              <Input
                id="channel-concurrency"
                type="number"
                min={0}
                value={form.maxConcurrency}
                disabled={readOnly}
                aria-invalid={!!errors.maxConcurrency}
                onChange={(event) => patch({ maxConcurrency: event.target.value })}
              />
            </FormField>
          </div>
          <p className="text-muted-foreground text-xs">0 或留空表示不限。</p>
        </Zone>

        <Zone title="高级与安全（会记入审计日志）" risk>
          <RiskRow
            id="channel-trusted"
            title="允许访问内网"
            field="trusted_internal"
            checked={form.trustedInternal}
            disabled={readOnly}
            description="允许 base_url 解析到内网地址（自建网关需要）。开启后，这个渠道的插件请求可以访问内网。此操作会记入审计日志。"
            onChange={(checked) =>
              checked ? openRisk("trusted") : patch({ trustedInternal: false })
            }
          />
          <RiskRow
            id="channel-cred"
            title="允许插件读取 Key"
            field="allow_credentials"
            checked={form.allowCredentials}
            disabled={readOnly || (!isCustomAuth && !form.allowCredentials)}
            description={
              isCustomAuth || form.allowCredentials
                ? "开启后，插件代码能读取这个渠道的 Key（用于自行签名）。请确认你信任这个插件。此操作会记入审计日志。"
                : "所选插件版本不需要接触 Key（鉴权由宿主注入），此开关不可用。"
            }
            onChange={(checked) =>
              checked ? openRisk("cred") : patch({ allowCredentials: false })
            }
          />
        </Zone>

        {original ? (
          <Zone title="Key">
            <div className="flex flex-wrap items-center gap-2">
              {original.secret_set ? (
                <Tag tone="success">已设置</Tag>
              ) : (
                <Tag tone="warning">未设置</Tag>
              )}
              <span className="text-muted-foreground text-xs">
                {original.secret_set
                  ? `上次更新 ${formatTime(original.updated_at)}`
                  : "未设置 Key 的渠道，模型无法发布"}
              </span>
              {!readOnly && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="ml-auto"
                  onClick={() => setSecretOpen(true)}
                >
                  <KeyRound />
                  {original.secret_set ? "更新 Key" : "设置 Key"}
                </Button>
              )}
            </div>
            <p className="text-muted-foreground text-xs">
              Key 只写不读：保存后不会再显示，也不会出现在任何请求地址或缓存里。
            </p>
            {(justSetKey || checkOk) && original.secret_set && (
              <Notice tone="success" title="Key 已设置">
                下一步：检查连通性，或{" "}
                <Link className="underline" to="/admin/ai/models/new">
                  去上架模型
                </Link>
                。
              </Notice>
            )}
          </Zone>
        ) : (
          !readOnly && <Notice tone="info">保存渠道后，再在这里设置 Key。</Notice>
        )}

        {original && !readOnly && check && (
          <Zone title="连通性检查">
            <CheckResult state={check} />
          </Zone>
        )}
      </form>

      <SheetFooter className="flex-row items-center border-t">
        {original && !readOnly && (
          <Button
            type="button"
            variant="outline"
            disabled={check?.busy}
            onClick={() => onCheck(original.key)}
          >
            {check?.busy ? <Loader2 className="animate-spin" /> : <Stethoscope />}
            检查连通性
          </Button>
        )}
        <div className="ml-auto flex items-center gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            {readOnly ? "关闭" : "取消"}
          </Button>
          {!readOnly && (
            <Button type="submit" form="channel-form" disabled={saving}>
              {saving && <Loader2 className="animate-spin" />}
              保存
            </Button>
          )}
        </div>
      </SheetFooter>

      <ConfirmDialog
        open={risk !== null}
        title={risk === "cred" ? "开启“允许插件读取 Key”？" : "开启“允许访问内网”？"}
        destructive
        confirmLabel="开启"
        confirmDisabled={!riskChecked}
        description={
          risk === "cred" ? (
            <>
              开启后，<b>{pluginLabel}</b>
              {version && (
                <>
                  （sha256 <span className="font-mono">{shortSha(version.sha256)}</span>）
                </>
              )}
              的代码能读取这个渠道的
              Key。插件代码未经过评审，请确认你信任这份代码。此操作会记入审计日志。
            </>
          ) : (
            <>
              开启后，这个渠道的插件请求可以访问内网地址。请确认{" "}
              <span className="font-mono">{form.baseUrl.trim() || "（未填写）"}</span>{" "}
              是受信任的内部服务。此操作会记入审计日志。
            </>
          )
        }
        onConfirm={confirmRisk}
        onCancel={() => setRisk(null)}
      >
        <label className="flex items-center gap-2 text-sm">
          <Checkbox
            checked={riskChecked}
            onCheckedChange={(checked) => setRiskChecked(checked === true)}
          />
          {risk === "cred" ? "我确认信任这份插件代码" : "我确认这个地址是受信任的内部服务"}
        </label>
      </ConfirmDialog>

      {original && !readOnly && (
        <SecretDialog
          open={secretOpen}
          channelKey={original.key}
          channelName={original.name}
          secretSet={original.secret_set}
          onClose={() => setSecretOpen(false)}
          onSaved={() => void refreshAfterSecret()}
        />
      )}
    </>
  );
}
