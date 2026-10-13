import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import {
  Boxes,
  ChevronRight,
  Loader2,
  Save,
  SlidersHorizontal,
  Stethoscope,
  X,
} from "lucide-react";

import { createChannel, getChannel, setChannelSecret, updateChannel } from "@/api/admin/ai";
import type { ChannelOption, ChannelView, PluginView } from "@/api/admin/ai/type.d";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  buildChannelRequest,
  buildCheckDraftRequest,
  canSaveChannel,
  connectionFingerprint,
  channelFormFromView,
  emptyChannelForm,
  rebaseSettings,
  suggestChannelKey,
  type ChannelFormState,
} from "@/utils/admin/channel-form";
import { errorMessage, isChannelKeyExists } from "@/utils/admin/errors";
import { availableUpgrade, findPluginVersion, latestVersion, shortSha } from "@/utils/admin/plugin";
import { settingFields, type SettingFormValue } from "@/utils/admin/settings-form";
import { formatTime } from "@/utils/time";

import { useRetained } from "@/hooks/use-retained";
import { SettingFields } from "../setting-fields";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { InitialAvatar } from "@/components/admin-ui/initial-avatar";
import { UnderlineTab, UnderlineTabs } from "@/components/admin-ui/underline-tabs";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { Notice } from "@/components/admin-ui/notice";
import { StatusDot } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import { KindFilter, KindIcons } from "../kind";
import { HEALTH_UI_TONE } from "@/utils/admin/health";
import type { ChannelTab } from "@/utils/admin/channel-route";
import { useAliveRef } from "../../use-admin";
import { ChannelModels, type ChannelModelActions } from "./channel-models";
import { CheckResult } from "./check-result";
import { useDraftCheck } from "./use-channel-check";

/** 弹窗打开的对象：新建（可预选插件），或已有渠道 */
export type ChannelDialogTarget =
  | { kind: "new"; pluginKey?: string }
  | { kind: "edit"; channel: ChannelView };

/** 一个分块：标题 + 一句说明 + 内容；risk 的内容包进琥珀描边框（安全开关） */
function Zone({
  title,
  description,
  risk,
  children,
}: {
  title: string;
  description?: string;
  risk?: boolean;
  children: ReactNode;
}) {
  return (
    <FormSection>
      <FormSectionHeader>
        <FormSectionTitle>{title}</FormSectionTitle>
        {description && <FormSectionDescription>{description}</FormSectionDescription>}
      </FormSectionHeader>
      <div
        className={cn(
          "flex flex-col gap-3",
          risk && "divide-y rounded-lg border border-amber-500/60 p-3.5 [&>*+*]:pt-3",
        )}
      >
        {children}
      </div>
    </FormSection>
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
 * 渠道弹窗：新建 / 编辑；admin 打开为只读（写操作不渲染）。
 * 已有渠道分两个页签：配置（表单）、模型（使用它的模型）；新建只有配置内容。
 * 配置页一屏三步：1 选协议（下拉，可按类型筛选）→ 2 连接（地址、Key、插件设置）→ 3 名称（标识按名称自动生成，可改）；
 * 插件版本、限流、启用、安全开关收在“高级设置”里（有新版本时默认展开）。
 * 底部是“检查连通性”（只检查已保存的渠道，通过后给“导入模型 / 新建模型”两个下一步）和“保存”。
 * “允许插件读取 Key”从关到开要勾选确认；新建时插件是 auth: custom 则它自动打开（必需）。
 * 配置页签保持挂载，切页签不丢未保存的修改。
 * @param initialTab 打开时停在哪个页签
 * @param channels 全部渠道（options）：取最新的健康状态和字段值，保存后不用等弹窗重开；自动生成标识时避开已有的
 * @param modelsRefreshKey 递增即重新拉取“模型”页签（模型有改动之后）
 * @param pluginsReady 插件清单已加载（未就绪时显示骨架，避免版本下拉是空的）
 * @param onSaved 新建 / 更新 / 设 Key 后，通知列表刷新
 */
export function ChannelDialog({
  target,
  initialTab,
  plugins,
  pluginsReady,
  canWrite,
  channels,
  modelsRefreshKey,
  modelActions,
  onSaved,
  onClose,
  children,
}: {
  target: ChannelDialogTarget | null;
  initialTab: ChannelTab;
  plugins: PluginView[];
  pluginsReady: boolean;
  canWrite: boolean;
  channels: ChannelOption[];
  modelsRefreshKey: number;
  modelActions: ChannelModelActions;
  onSaved: () => void;
  onClose: () => void;
  /** 挂在弹窗内部的子弹窗（模型编辑），放在这里才算嵌套，点子弹窗不会被当成点了外面 */
  children?: ReactNode;
}) {
  /** 由表单上报“是否有未保存修改”，关闭时据此确认 */
  const [dirty, setDirty] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);

  const requestClose = () => {
    if (dirty) setConfirmClose(true);
    else onClose();
  };
  // 关闭时 target 会被置空，弹窗内容按最后一次的对象留到退出动画播完
  const shown = useRetained(target);

  return (
    <>
      <Dialog open={!!target} onOpenChange={(next) => !next && requestClose()}>
        <DialogContent
          showCloseButton={false}
          className="flex h-[min(88svh,800px)] max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl"
        >
          {shown && (
            <DialogBody
              key={shown.kind === "edit" ? shown.channel.key : `new:${shown.pluginKey ?? ""}`}
              target={shown}
              initialTab={initialTab}
              plugins={plugins}
              pluginsReady={pluginsReady}
              canWrite={canWrite}
              channels={channels}
              modelsRefreshKey={modelsRefreshKey}
              modelActions={modelActions}
              onSaved={onSaved}
              onDirtyChange={setDirty}
              onClose={requestClose}
              onFinished={() => {
                setDirty(false);
                onClose();
              }}
            />
          )}
          {children}
        </DialogContent>
      </Dialog>
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

type DialogBodyProps = {
  target: ChannelDialogTarget;
  initialTab: ChannelTab;
  plugins: PluginView[];
  pluginsReady: boolean;
  canWrite: boolean;
  channels: ChannelOption[];
  modelsRefreshKey: number;
  modelActions: ChannelModelActions;
  onSaved: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onClose: () => void;
  /** 保存成功：直接关闭弹窗（已没有未保存的修改，不再确认） */
  onFinished: () => void;
};

function DialogBody({ target, pluginsReady, canWrite, plugins, ...rest }: DialogBodyProps) {
  const aliveRef = useAliveRef();
  /** 已存在的渠道；新建成功后会变成刚创建的那个，弹窗转为编辑态 */
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
        <DialogHeader className="border-b px-4 py-3">
          <DialogTitle>渠道</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3 p-4" aria-busy="true">
          <Skeleton className="h-24" />
          <Skeleton className="h-40" />
        </div>
      </>
    );
  }

  return (
    <DialogForm
      aliveRef={aliveRef}
      initialForm={initialForm}
      original={original}
      setOriginal={setOriginal}
      plugins={plugins}
      readOnly={readOnly}
      existingKeys={rest.channels.map((item) => item.key)}
      {...rest}
    />
  );
}

function DialogForm({
  aliveRef,
  initialForm,
  original,
  setOriginal,
  plugins,
  existingKeys,
  readOnly,
  initialTab,
  channels,
  modelsRefreshKey,
  modelActions,
  onSaved,
  onDirtyChange,
  onClose,
  onFinished,
}: Omit<DialogBodyProps, "target" | "pluginsReady" | "canWrite"> & {
  aliveRef: { current: boolean };
  initialForm: ChannelFormState;
  original: ChannelView | null;
  setOriginal: (channel: ChannelView) => void;
  existingKeys: string[];
  readOnly: boolean;
}) {
  const isNew = !original;
  const { state: check, run: runCheck } = useDraftCheck();
  const [tab, setTab] = useState<ChannelTab>(isNew ? "config" : initialTab);
  const changeTab = setTab;
  /** 最新的渠道（列表刷新后的值），保存之后不用等弹窗重开 */
  const option = original ? channels.find((item) => item.key === original.key) : undefined;
  const live: ChannelView | null = original ? (option ?? original) : null;
  const health = option?.health ?? null;
  /** “模型”页签上的数量角标：页签挂载后由它自己上报；没打开过时为 null */
  const [modelCount, setModelCount] = useState<number | null>(null);
  const [form, setFormState] = useState<ChannelFormState>(initialForm);
  const [baseline, setBaseline] = useState(() => JSON.stringify(initialForm));
  /** Key 明文只在这里，提交时取走并立刻清空；不进 form，不参与脏比较的 JSON */
  const [secret, setSecret] = useState("");
  /** 新建时用户手动改过标识：之后不再按名称自动生成 */
  const [keyEdited, setKeyEdited] = useState(false);
  const [pluginKind, setPluginKind] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  /** 已保存的连接配置指纹：和当前指纹一致就不用重新检查（见 canSaveChannel） */
  const [baselineFp, setBaselineFp] = useState(() => connectionFingerprint(initialForm, ""));
  const [dropped, setDropped] = useState<string[]>([]);
  const [credsReset, setCredsReset] = useState(false);
  /** “允许插件读取 Key”的确认弹窗是否打开，以及里面的确认勾选 */
  const [credConfirmOpen, setCredConfirmOpen] = useState(false);
  const [credConfirmChecked, setCredConfirmChecked] = useState(false);

  /** 弹窗卸载（关闭、切换对象）时清掉脏标记，避免下次打开误提示“放弃修改” */
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const setForm = (next: ChannelFormState, nextSecret = secret) => {
    setFormState(next);
    onDirtyChange(JSON.stringify(next) !== baseline || nextSecret.trim() !== "");
  };
  const patch = (partial: Partial<ChannelFormState>) => setForm({ ...form, ...partial });

  const plugin = plugins.find((item) => item.key === form.pluginKey);
  const version = findPluginVersion(plugins, form.pluginKey, {
    version: form.pluginVersion,
  });
  const meta = version?.meta ?? undefined;
  const fields = useMemo(() => settingFields(meta?.channelSettings), [meta]);
  const authType = meta?.auth?.type ?? "none";
  const needsKey = authType !== "none";
  const isCustomAuth = authType === "custom";
  const upgrade = original
    ? availableUpgrade(plugins, {
        plugin_key: form.pluginKey,
        plugin_version: form.pluginVersion,
      })
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
    const nextVersion = findPluginVersion(plugins, pluginKey, {
      version: versionText,
    });
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

  /** 插件是否支持某种类型 */
  const supportsKind = (item: PluginView, kind: string) =>
    kind in (latestVersion(item)?.meta?.endpoints ?? {});

  /**
   * 切协议类型筛选：下拉里只剩这个类型的协议；如果当前选中的不属于它，就改选这个类型里第一个可用的协议（新建、编辑都一样）。
   */
  const changePluginKind = (kind: string) => {
    setPluginKind(kind);
    if (!kind) return;
    const matches = plugins.filter((item) => item.enabled && supportsKind(item, kind));
    const current = plugins.find((item) => item.key === form.pluginKey);
    if (matches.length > 0 && !(current && supportsKind(current, kind))) {
      onPluginChange(matches[0].key);
    }
  };

  const openCredConfirm = () => {
    setCredConfirmChecked(false);
    setCredConfirmOpen(true);
  };

  const confirmCred = () => {
    patch({ allowCredentials: true });
    setCredConfirmOpen(false);
  };

  /**
   * 保存：新建 / 更新渠道（没改动就跳过），填了 Key 就写入（只写不读，明文提交后立刻清空）。
   * 连通性检查是单独的按钮（只检查已保存的渠道）。
   * 第 1 步成功、第 2 步失败时渠道已经保存，弹窗转为编辑态，失败原因留在顶部。
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
      setBaselineFp(connectionFingerprint(effective, ""));
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
    onFinished();
  };

  const changeSetting = (name: string, value: SettingFormValue) =>
    setForm({ ...form, settings: { ...form.settings, [name]: value } });

  const pluginLabel = plugin ? `${plugin.name} v${form.pluginVersion}` : form.pluginKey;
  const checking = !!check?.busy;
  const keyMissing = !!original && needsKey && !original.secret_set;
  /** 当前连接配置的指纹；检查结果只对检查时的那份配置有效 */
  const fingerprint = connectionFingerprint(effective, secret);
  const freshCheck = check && (check.busy || check.fingerprint === fingerprint) ? check : undefined;
  /** 保存前必须先通过连通性检查（编辑时连接信息没动除外） */
  const canSave = canSaveChannel({ isNew, fingerprint, baseline: baselineFp, check });
  /** 用表单里还没保存的配置检查连通性：只校验连接相关字段，不落库 */
  const checkConnection = () => {
    const built = buildCheckDraftRequest(effective, fields, secret, original?.key);
    setFormError(null);
    if (!built.ok) {
      setErrors(built.errors);
      return;
    }
    setErrors({});
    void runCheck(built.request, fingerprint);
  };
  const settingErrors = Object.fromEntries(
    Object.entries(errors)
      .filter(([key]) => key.startsWith("settings."))
      .map(([key, message]) => [key.slice("settings.".length), message]),
  );

  return (
    <>
      <div className="flex items-start gap-4 border-b px-6 py-4">
        <InitialAvatar
          name={live ? live.name : "+"}
          seed={live ? live.key : "new"}
          className="size-10 text-base"
        />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle className="text-lg font-semibold">
              {live ? live.name : "新建渠道"}
            </DialogTitle>
            {live && health && (
              <Tag tone={HEALTH_UI_TONE[health.tone]} title={health.reason || undefined}>
                <StatusDot tone={HEALTH_UI_TONE[health.tone]} />
                {health.label}
              </Tag>
            )}
          </div>
          <DialogDescription className="text-muted-foreground mt-0.5 truncate text-sm">
            {live ? (
              <>
                <span className="font-mono">{live.key}</span>
                {" · "}
                {plugins.find((item) => item.key === live.plugin_key)?.name ?? live.plugin_key}
              </>
            ) : null}
          </DialogDescription>
        </div>
        <Button type="button" variant="ghost" size="icon-sm" aria-label="关闭" onClick={onClose}>
          <X />
        </Button>
      </div>

      {live && (
        <UnderlineTabs>
          <UnderlineTab selected={tab === "config"} onClick={() => changeTab("config")}>
            <SlidersHorizontal />
            配置
          </UnderlineTab>
          <UnderlineTab selected={tab === "models"} onClick={() => changeTab("models")}>
            <Boxes />
            模型
            {modelCount !== null && (
              <span className="text-muted-foreground text-xs tabular-nums">{modelCount}</span>
            )}
          </UnderlineTab>
        </UnderlineTabs>
      )}

      <div className="flex min-h-0 flex-1 flex-col">
        {live && tab === "models" && (
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
            <ChannelModels
              channel={live}
              channels={channels}
              kinds={option?.kinds ?? []}
              refreshKey={modelsRefreshKey}
              actions={modelActions}
              onTotal={setModelCount}
            />
          </div>
        )}
        {/* 配置页保持挂载，切页签不丢未保存的修改 */}
        <div className={cn("min-h-0 flex-1", tab !== "config" && "hidden")}>
          <div className="flex h-full min-h-0 flex-col">
            <form
              id="channel-form"
              className="min-h-0 flex-1 overflow-y-auto px-6 py-5"
              onSubmit={(event) => {
                event.preventDefault();
                if (!readOnly && canSave) void save();
              }}
            >
              {formError && (
                <Notice tone="danger" title="没有完成" className="mb-4">
                  {formError}
                </Notice>
              )}

              {keyMissing && (
                <Notice tone="warning" title="这个渠道还没有 Key" className="mb-4">
                  填上 API Key，检查连通性通过后保存；没有 Key 的渠道，模型无法上线。
                </Notice>
              )}

              <Zone title="协议">
                {!readOnly && (
                  <KindFilter
                    value={pluginKind}
                    onChange={changePluginKind}
                    slideId="channel-plugin-kind"
                  />
                )}
                <Select
                  value={form.pluginKey}
                  disabled={readOnly}
                  onValueChange={(value) => value && onPluginChange(value)}
                >
                  <SelectTrigger
                    className="w-full"
                    aria-label="协议"
                    aria-invalid={!!errors.plugin}
                  >
                    <SelectValue placeholder="请选择协议">
                      {(value: string) => {
                        const item = plugins.find((p) => p.key === value);
                        return item ? item.name : value;
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    {plugins
                      .filter(
                        (item) =>
                          !pluginKind ||
                          supportsKind(item, pluginKind) ||
                          item.key === form.pluginKey,
                      )
                      .map((item) => (
                        <SelectItem
                          key={item.key}
                          value={item.key}
                          disabled={!item.enabled && item.key !== form.pluginKey}
                        >
                          <span className="flex flex-1 items-center justify-between gap-3">
                            <span>
                              {item.name}
                              {!item.enabled && "（已停用）"}
                            </span>
                            <KindIcons
                              kinds={Object.keys(latestVersion(item)?.meta?.endpoints ?? {})}
                            />
                          </span>
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
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

              <Zone title="连接">
                <div className="grid gap-4 sm:grid-cols-2">
                  <FormField label="接口地址" htmlFor="channel-base-url" error={errors.baseUrl}>
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
                        original?.secret_set && secret.trim() ? "保存时会覆盖现有 Key" : undefined
                      }
                    >
                      <Input
                        id="channel-secret"
                        type="password"
                        autoComplete="new-password"
                        className="font-mono"
                        value={secret}
                        placeholder={
                          original?.secret_set ? "已设置。要更换就填新的，留空不改" : "sk-..."
                        }
                        onChange={(event) => {
                          setSecret(event.target.value);
                          setForm(form, event.target.value);
                        }}
                      />
                    </FormField>
                  )}
                </div>
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

              <Zone title="名称">
                <div className="grid gap-4 sm:grid-cols-2">
                  <FormField label="显示名称" htmlFor="channel-name" error={errors.name}>
                    <Input
                      id="channel-name"
                      value={form.name}
                      placeholder="如 OpenAI 主线"
                      disabled={readOnly}
                      aria-invalid={!!errors.name}
                      onChange={(event) => patch({ name: event.target.value })}
                    />
                  </FormField>
                  {isNew && !keyEdited ? null : (
                    <FormField
                      label="标识 key"
                      htmlFor="channel-key"
                      error={errors.key}
                      hint={isNew ? "小写字母、数字、连字符；创建后不可修改" : undefined}
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
                </div>
              </Zone>

              <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
                <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex w-full items-center gap-1.5 py-1 text-sm">
                  <ChevronRight
                    className={cn("size-4 transition-transform", advancedOpen && "rotate-90")}
                  />
                  高级设置
                  {upgrade && <Tag>有新版本 {upgrade.version}</Tag>}
                </CollapsibleTrigger>
                <CollapsibleContent className="mt-2 flex flex-col gap-3.5">
                  <Zone title="插件版本">
                    <FormField label="固定版本" htmlFor="channel-version">
                      <OptionSelect
                        id="channel-version"
                        value={form.pluginVersion}
                        disabled={readOnly}
                        placeholder="请选择"
                        onValueChange={(value) => changePluginVersion(form.pluginKey, value)}
                        options={[
                          ...(version
                            ? []
                            : [
                                {
                                  value: form.pluginVersion,
                                  label: form.pluginVersion || "请选择",
                                },
                              ]),
                          ...(plugin?.versions ?? []).map((item) => ({
                            value: item.version,
                            label: item.version,
                          })),
                        ]}
                      />
                    </FormField>
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
                      <Notice tone="warning">
                        新版本不需要接触 Key，已自动关闭“允许插件读取 Key”。
                      </Notice>
                    )}
                  </Zone>

                  <Zone title="限流与启用">
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

                  <Zone title="安全开关" description="会记入审计日志" risk>
                    <RiskRow
                      id="channel-cred"
                      title="允许插件读取 Key"
                      field="allow_credentials"
                      checked={effective.allowCredentials}
                      disabled={
                        readOnly ||
                        (isNew && isCustomAuth) ||
                        (!isCustomAuth && !form.allowCredentials)
                      }
                      description={
                        isNew && isCustomAuth
                          ? "这个插件要用 Key 自己签名，必须打开。"
                          : isCustomAuth || form.allowCredentials
                            ? "开启后，插件代码能读取这个渠道的 Key（用于自行签名）。请确认你信任这个插件。"
                            : "所选插件版本不需要接触 Key（鉴权由宿主注入），此开关不可用。"
                      }
                      onChange={(checked) =>
                        checked ? openCredConfirm() : patch({ allowCredentials: false })
                      }
                    />
                  </Zone>
                </CollapsibleContent>
              </Collapsible>
            </form>

            <div className="flex flex-wrap items-center gap-3 border-t px-6 py-3.5">
              {readOnly && (
                <span className="text-muted-foreground text-xs leading-tight">
                  只读 · 需要运维权限
                </span>
              )}
              {!readOnly && freshCheck && (
                <CheckResult
                  state={freshCheck}
                  plugin={{ key: form.pluginKey, version: form.pluginVersion }}
                />
              )}
              <div className="ml-auto flex items-center gap-2">
                {!readOnly && (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={saving || checking}
                    onClick={checkConnection}
                  >
                    {checking ? <Loader2 className="animate-spin" /> : <Stethoscope />}
                    {checking ? "检查中…" : "检查连通性"}
                  </Button>
                )}
                <Button type="button" variant="outline" onClick={onClose}>
                  {readOnly ? "关闭" : "取消"}
                </Button>
                {!readOnly && (
                  <Button
                    type="submit"
                    form="channel-form"
                    disabled={saving || checking || !canSave}
                    title={canSave ? undefined : "先通过连通性检查才能保存"}
                  >
                    {saving ? <Loader2 className="animate-spin" /> : <Save />}
                    {saving ? "保存中…" : "保存"}
                  </Button>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={credConfirmOpen}
        title="开启“允许插件读取 Key”？"
        destructive
        confirmLabel="开启"
        confirmDisabled={!credConfirmChecked}
        description={
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
        }
        onConfirm={confirmCred}
        onCancel={() => setCredConfirmOpen(false)}
      >
        <label className="flex items-center gap-2 text-sm">
          <Checkbox
            checked={credConfirmChecked}
            onCheckedChange={(checked) => setCredConfirmChecked(checked === true)}
          />
          我确认信任这份插件代码
        </label>
      </ConfirmDialog>
    </>
  );
}
