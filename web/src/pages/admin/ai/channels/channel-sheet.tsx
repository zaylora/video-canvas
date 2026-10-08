import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { ChevronRight, Download, Loader2, Plus, Stethoscope } from "lucide-react";

import { createChannel, getChannel, setChannelSecret, updateChannel } from "@/api/admin/ai";
import type { ChannelView, PluginView } from "@/api/admin/ai/type.d";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
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
  suggestChannelKey,
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

import { useRetained } from "@/hooks/use-retained";
import { SettingFields } from "../setting-fields";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { ChoiceCard, ChoiceCardGroup } from "@/components/admin-ui/choice-card";
import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import { KindIcons } from "../kind";
import { ReadOnlyNotice } from "../../shared";
import { useAliveRef } from "../../use-admin";
import { CheckResult } from "./check-result";
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
 * 一屏三步：1 选平台（插件卡片）→ 2 连接（地址、Key、插件设置）→ 3 名称（标识按名称自动生成，可改）；
 * 插件版本、限流、启用、安全开关收在“高级设置”里（有新版本时默认展开）。
 * 底部只有一个“保存并检查”：保存渠道 → 写 Key → 检查连通性，通过后给“导入模型 / 新建模型”两个下一步。
 * 两个危险开关从关到开要勾选确认；新建时插件是 auth: custom 则“允许插件读取 Key”自动打开（必需）。
 * @param pluginsReady 插件清单已加载（未就绪时抽屉里显示骨架，避免版本下拉是空的）
 * @param existingKeys 已有渠道的 key（自动生成标识时避开）
 * @param onSaved 新建 / 更新 / 设 Key 后，通知列表刷新
 * @param onImport 检查通过后点“从这个渠道导入模型”（页面负责关抽屉、开导入弹窗）
 */
export function ChannelSheet({
  target,
  plugins,
  pluginsReady,
  canWrite,
  checks,
  existingKeys,
  onCheck,
  onSaved,
  onImport,
  onClose,
}: {
  target: ChannelSheetTarget | null;
  plugins: PluginView[];
  pluginsReady: boolean;
  canWrite: boolean;
  /** 各渠道的检查状态（按 key）；新建成功后抽屉转为编辑态，要按新 key 取 */
  checks: Record<string, CheckState>;
  existingKeys: string[];
  onCheck: (key: string) => void;
  onSaved: () => void;
  onImport?: (channel: ChannelView) => void;
  onClose: () => void;
}) {
  /** 由 Body 上报“是否有未保存修改”，关闭时据此确认 */
  const [dirty, setDirty] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);

  const requestClose = () => {
    if (dirty) setConfirmClose(true);
    else onClose();
  };
  // 关闭时 target 会被置空，抽屉内容按最后一次的对象留到滑出动画播完
  const shown = useRetained(target);

  return (
    <>
      <Sheet open={!!target} onOpenChange={(next) => !next && requestClose()}>
        <SheetContent className="w-full data-[side=right]:sm:max-w-xl">
          {shown && (
            <SheetBody
              key={shown.kind === "edit" ? shown.channel.key : `new:${shown.pluginKey ?? ""}`}
              target={shown}
              plugins={plugins}
              pluginsReady={pluginsReady}
              canWrite={canWrite}
              checks={checks}
              onCheck={onCheck}
              onSaved={onSaved}
              existingKeys={existingKeys}
              onImport={onImport}
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
  existingKeys,
  onCheck,
  onSaved,
  onImport,
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
  existingKeys: string[];
  onImport?: (channel: ChannelView) => void;
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
      existingKeys={existingKeys}
      onImport={onImport}
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
  existingKeys,
  readOnly,
  checks,
  onCheck,
  onSaved,
  onImport,
  onDirtyChange,
  onClose,
}: {
  aliveRef: { current: boolean };
  initialForm: ChannelFormState;
  original: ChannelView | null;
  setOriginal: (channel: ChannelView) => void;
  plugins: PluginView[];
  existingKeys: string[];
  readOnly: boolean;
  checks: Record<string, CheckState>;
  onCheck: (key: string) => void;
  onSaved: () => void;
  onImport?: (channel: ChannelView) => void;
  onDirtyChange: (dirty: boolean) => void;
  onClose: () => void;
}) {
  const isNew = !original;
  const check = original ? checks[original.key] : undefined;
  const [form, setFormState] = useState<ChannelFormState>(initialForm);
  const [baseline, setBaseline] = useState(() => JSON.stringify(initialForm));
  /** Key 明文只在这里，提交时取走并立刻清空；不进 form，不参与脏比较的 JSON */
  const [secret, setSecret] = useState("");
  /** 新建时用户手动改过标识：之后不再按名称自动生成 */
  const [keyEdited, setKeyEdited] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  /** 这次打开抽屉后点过“保存并检查”：检查通过时给下一步 */
  const [checked, setChecked] = useState(false);
  const [dropped, setDropped] = useState<string[]>([]);
  const [credsReset, setCredsReset] = useState(false);
  const [risk, setRisk] = useState<"trusted" | "cred" | null>(null);
  const [riskChecked, setRiskChecked] = useState(false);

  /** 抽屉卸载（关闭、切换对象）时清掉脏标记，避免下次打开误提示“放弃修改” */
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const setForm = (next: ChannelFormState, nextSecret = secret) => {
    setFormState(next);
    onDirtyChange(JSON.stringify(next) !== baseline || nextSecret.trim() !== "");
  };
  const patch = (partial: Partial<ChannelFormState>) => setForm({ ...form, ...partial });

  const plugin = plugins.find((item) => item.key === form.pluginKey);
  const version = findPluginVersion(plugins, form.pluginKey, { version: form.pluginVersion });
  const meta = version?.meta ?? undefined;
  const fields = useMemo(() => settingFields(meta?.channelSettings), [meta]);
  const authType = meta?.auth?.type ?? "none";
  const needsKey = authType !== "none";
  const isCustomAuth = authType === "custom";
  const upgrade = original
    ? availableUpgrade(plugins, { plugin_key: form.pluginKey, plugin_version: form.pluginVersion })
    : null;
  const switchedVersion = !!original && form.pluginVersion !== original.plugin_version;
  const [advancedOpen, setAdvancedOpen] = useState(!!upgrade);

  // 新建时：标识没手动改过就按名称自动生成；插件要自己签名（auth: custom）时必须允许读 Key，直接打开
  const effective: ChannelFormState = {
    ...form,
    key:
      isNew && !keyEdited
        ? suggestChannelKey(form.name, form.pluginKey, form.baseUrl, existingKeys)
        : form.key,
    allowCredentials: isNew && isCustomAuth ? true : form.allowCredentials,
  };

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
    setCredsReset(reset && !isNew);
    setForm({
      ...form,
      pluginKey,
      pluginVersion: versionText,
      settings: nextSettings,
      allowCredentials: reset ? false : form.allowCredentials,
    });
  };

  const onPluginChange = (key: string) => {
    if (key === form.pluginKey) return;
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

  /**
   * 保存并检查，一步完成：
   * 1. 新建 / 更新渠道（没改动就跳过）；
   * 2. 填了 Key 就写入（只写不读，明文提交后立刻清空）；
   * 3. 插件不需要 Key，或 Key 已设置，就跑一次连通性检查，结果显示在抽屉里，通过后给下一步。
   * 第 1 步成功、第 2 步失败时渠道已经保存，抽屉转为编辑态，失败原因留在顶部。
   */
  const save = async () => {
    const built = buildChannelRequest(effective, fields, original);
    if (!built.ok) {
      setErrors(built.errors);
      setFormError(null);
      if (built.errors.key) setKeyEdited(true);
      if (built.errors.rps || built.errors.maxConcurrency || built.errors.maxRunning)
        setAdvancedOpen(true);
      return;
    }
    setErrors({});
    setFormError(null);
    setSaving(true);
    let current = original;
    try {
      if (!original) current = await createChannel(built.create);
      else if (built.changed) current = await updateChannel(original.key, built.update);
      if (!aliveRef.current || !current) return;
      setOriginal(current);
      setForm(effective, secret);
      setBaseline(JSON.stringify(effective));
      setKeyEdited(true);
      setDropped([]);
      setCredsReset(false);
      onSaved();
    } catch (error) {
      // 全局 toast 已弹；这里把原因就地放在表单里（50012 定位到 key 字段）
      if (!aliveRef.current) return;
      if (isChannelKeyExists(error)) {
        setKeyEdited(true);
        setErrors({ key: errorMessage(error, "这个标识已经存在") });
      } else setFormError(errorMessage(error, "保存失败"));
      setSaving(false);
      return;
    }

    const plain = secret.trim();
    if (plain) {
      setSecret("");
      try {
        await setChannelSecret(current.key, plain);
        const fresh = await getChannel(current.key);
        if (!aliveRef.current) return;
        current = fresh;
        setOriginal(fresh);
        onSaved();
      } catch (error) {
        if (aliveRef.current) {
          setFormError(`渠道已保存，但 Key 没设置成功：${errorMessage(error, "请重试")}`);
          setSaving(false);
        }
        return;
      }
    }
    if (!aliveRef.current) return;
    onDirtyChange(false);
    setSaving(false);
    setChecked(true);
    if (!needsKey || current.secret_set) onCheck(current.key);
  };

  const changeSetting = (name: string, value: SettingFormValue) =>
    setForm({ ...form, settings: { ...form.settings, [name]: value } });

  const title = isNew ? "新建渠道" : readOnly ? "查看渠道" : "编辑渠道";
  const pluginLabel = plugin ? `${plugin.name} v${form.pluginVersion}` : form.pluginKey;
  const checkOk = !!check && !check.busy && check.outcome.kind === "ok";
  const checking = !!check?.busy;
  const keyMissing = !!original && needsKey && !original.secret_set;
  const settingErrors = Object.fromEntries(
    Object.entries(errors)
      .filter(([key]) => key.startsWith("settings."))
      .map(([key, message]) => [key.slice("settings.".length), message]),
  );

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
        <SheetDescription>
          {isNew
            ? "选平台 → 填地址和 Key → 起个名字，点「保存并检查」就能用。"
            : "渠道把一个插件版本、一个地址和一个 Key 绑在一起。"}
        </SheetDescription>
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
          <Notice tone="danger" title="没有完成">
            {formError}
          </Notice>
        )}

        {checked && original && (checkOk || checking || check) && (
          <Zone title="连通性检查">
            {check && <CheckResult state={check} />}
            {checkOk && (
              <div className="flex flex-wrap gap-2">
                {meta?.import && onImport && (
                  <Button type="button" size="sm" onClick={() => onImport(original)}>
                    <Download />
                    从这个渠道导入模型
                  </Button>
                )}
                <Button
                  type="button"
                  size="sm"
                  variant={meta?.import && onImport ? "outline" : "default"}
                  render={
                    <Link to={`/admin/ai/models/new?channel=${encodeURIComponent(original.key)}`} />
                  }
                >
                  <Plus />
                  用这个渠道新建模型
                </Button>
              </div>
            )}
          </Zone>
        )}
        {checked && keyMissing && (
          <Notice tone="warning" title="渠道已保存，还没有 Key">
            填上 API Key 再点「保存并检查」；没有 Key 的渠道，模型无法上线。
          </Notice>
        )}

        <Zone title="1 · 选平台">
          <ChoiceCardGroup aria-label="插件" className="sm:grid-cols-2">
            {plugins.map((item) => {
              const latest = latestVersion(item);
              const auth = latest?.meta?.auth?.type ?? "none";
              return (
                <ChoiceCard
                  key={item.key}
                  indicator
                  selected={item.key === form.pluginKey}
                  disabled={readOnly || (!item.enabled && item.key !== form.pluginKey)}
                  onClick={() => onPluginChange(item.key)}
                >
                  <span className="flex min-w-0 flex-col gap-1">
                    <span className="truncate text-sm font-medium">
                      {item.name}
                      {!item.enabled && "（已停用）"}
                    </span>
                    <span className="text-muted-foreground flex items-center gap-2 text-xs">
                      <KindIcons kinds={Object.keys(latest?.meta?.endpoints ?? {})} />
                      {auth === "none" ? "无需 Key" : "需要 Key"}
                    </span>
                  </span>
                </ChoiceCard>
              );
            })}
          </ChoiceCardGroup>
          {errors.plugin && <p className="text-destructive text-xs">{errors.plugin}</p>}
          {plugins.length === 0 && (
            <p className="text-muted-foreground text-xs">
              还没有可用的插件，先到
              <Link className="mx-1 underline" to="/admin/ai/plugins">
                插件页
              </Link>
              上传一个。
            </p>
          )}
        </Zone>

        <Zone title="2 · 连接">
          <FormField
            label="接口地址"
            htmlFor="channel-base-url"
            error={errors.baseUrl}
            hint="插件请求只能发到这个地址；http/https，不能带用户名密码"
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
          {needsKey && !readOnly && (
            <FormField
              label={
                <span className="flex items-center gap-2">
                  API Key
                  {original &&
                    (original.secret_set ? (
                      <Tag tone="success">已设置 · {formatTime(original.updated_at)}</Tag>
                    ) : (
                      <Tag tone="warning">未设置</Tag>
                    ))}
                </span>
              }
              htmlFor="channel-secret"
              hint={
                original?.secret_set && secret.trim()
                  ? "保存时会覆盖现有 Key"
                  : "只写不读：保存后不会再显示，也不会出现在任何请求地址或缓存里"
              }
            >
              <Input
                id="channel-secret"
                type="password"
                autoComplete="new-password"
                className="font-mono"
                value={secret}
                placeholder={original?.secret_set ? "已设置。要更换就填新的，留空不改" : "sk-..."}
                onChange={(event) => {
                  setSecret(event.target.value);
                  setForm(form, event.target.value);
                }}
              />
            </FormField>
          )}
          {needsKey && readOnly && original && (
            <p className="text-muted-foreground text-xs">
              API Key：{original.secret_set ? "已设置" : "未设置"}
            </p>
          )}
          {!needsKey && version && (
            <p className="text-muted-foreground text-xs">这个插件不需要 Key。</p>
          )}
          {isNew && isCustomAuth && (
            <Notice tone="warning">
              这个插件要用 Key 自己签名，已自动打开“允许插件读取 Key”（会记入审计日志）。
            </Notice>
          )}
          {fields.length > 0 && (
            <SettingFields
              idPrefix="channel-setting"
              fields={fields}
              values={form.settings}
              errors={settingErrors}
              disabled={readOnly}
              onChange={changeSetting}
            />
          )}
        </Zone>

        <Zone title="3 · 名称">
          <FormField label="显示名称" htmlFor="channel-name" error={errors.name}>
            <Input
              id="channel-name"
              value={form.name}
              placeholder="如 NewAPI 主线"
              disabled={readOnly}
              aria-invalid={!!errors.name}
              onChange={(event) => patch({ name: event.target.value })}
            />
          </FormField>
          {isNew && !keyEdited ? (
            <p className="text-muted-foreground text-xs">
              标识自动生成：<code className="font-mono">{effective.key}</code>{" "}
              <button
                type="button"
                className="underline underline-offset-4"
                onClick={() => {
                  setKeyEdited(true);
                  patch({ key: effective.key });
                }}
              >
                修改
              </button>
            </p>
          ) : (
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
                disabled={!isNew || readOnly}
                aria-invalid={!!errors.key}
                onChange={(event) => patch({ key: event.target.value })}
              />
            </FormField>
          )}
        </Zone>

        <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
          <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex w-full items-center gap-1.5 py-1 text-sm">
            <ChevronRight
              className={cn("size-4 transition-transform", advancedOpen && "rotate-90")}
            />
            高级设置
            <span className="text-xs">（插件版本、限流、启用、安全开关）</span>
            {upgrade && <Tag tone="info">有新版本 {upgrade.version}</Tag>}
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-2 flex flex-col gap-3.5">
            <Zone title="插件版本">
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
              {version && (
                <p className="text-muted-foreground text-xs">
                  鉴权：{describeAuth(meta?.auth)} · sha256{" "}
                  <span className="font-mono">{shortSha(version.sha256)}</span>
                </p>
              )}
              {upgrade && (
                <Notice tone="info" title={`有新版本 ${upgrade.version}`}>
                  切换后新任务用新版本，进行中的任务按旧版本跑完。
                  <b>切换后建议先对相关模型试跑。</b>
                </Notice>
              )}
              {switchedVersion && !upgrade && (
                <Notice tone="info">
                  保存后新任务改用 v{form.pluginVersion}
                  ，进行中的任务按旧版本跑完。建议保存后先对使用这个渠道的模型试跑。
                </Notice>
              )}
              {dropped.length > 0 && (
                <Notice tone="warning">
                  新版本不再有这些设置项，已丢弃：{dropped.join("、")}。
                </Notice>
              )}
              {credsReset && (
                <Notice tone="warning">新版本不需要接触 Key，已自动关闭“允许插件读取 Key”。</Notice>
              )}
            </Zone>

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
                <FormField
                  label="最大同时请求数"
                  htmlFor="channel-concurrency"
                  error={errors.maxConcurrency}
                >
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
                <FormField
                  label="最大同时生成数"
                  htmlFor="channel-max-running"
                  error={errors.maxRunning}
                  className="col-span-2"
                >
                  <Input
                    id="channel-max-running"
                    type="number"
                    min={0}
                    value={form.maxRunning}
                    disabled={readOnly}
                    aria-invalid={!!errors.maxRunning}
                    onChange={(event) => patch({ maxRunning: event.target.value })}
                  />
                </FormField>
              </div>
              <p className="text-muted-foreground text-xs">
                0 或留空表示不限。「最大同时请求数」限制同时发给上游的 HTTP 请求；
                「最大同时生成数」限制同时在上游生成的任务数，填上游账号允许的并发，超出的任务在平台里显示“排队中”，等有空位再提交。
              </p>
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

            <Zone title="安全开关（会记入审计日志）" risk>
              <RiskRow
                id="channel-trusted"
                title="允许访问内网"
                field="trusted_internal"
                checked={form.trustedInternal}
                disabled={readOnly}
                description="允许接口地址解析到内网地址（自建网关需要）。开启后，这个渠道的插件请求可以访问内网。"
                onChange={(checked) =>
                  checked ? openRisk("trusted") : patch({ trustedInternal: false })
                }
              />
              <RiskRow
                id="channel-cred"
                title="允许插件读取 Key"
                field="allow_credentials"
                checked={effective.allowCredentials}
                disabled={
                  readOnly || (isNew && isCustomAuth) || (!isCustomAuth && !form.allowCredentials)
                }
                description={
                  isNew && isCustomAuth
                    ? "这个插件要用 Key 自己签名，必须打开。"
                    : isCustomAuth || form.allowCredentials
                      ? "开启后，插件代码能读取这个渠道的 Key（用于自行签名）。请确认你信任这个插件。"
                      : "所选插件版本不需要接触 Key（鉴权由宿主注入），此开关不可用。"
                }
                onChange={(checked) =>
                  checked ? openRisk("cred") : patch({ allowCredentials: false })
                }
              />
            </Zone>
          </CollapsibleContent>
        </Collapsible>
      </form>

      <SheetFooter className="flex-row items-center border-t">
        <div className="ml-auto flex items-center gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            {readOnly ? "关闭" : "取消"}
          </Button>
          {!readOnly && (
            <Button type="submit" form="channel-form" disabled={saving || checking}>
              {saving || checking ? <Loader2 className="animate-spin" /> : <Stethoscope />}
              {saving ? "保存中…" : checking ? "检查中…" : "保存并检查"}
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
    </>
  );
}
