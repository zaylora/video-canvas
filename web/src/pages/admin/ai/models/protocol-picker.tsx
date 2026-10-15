import { useMemo, useState } from "react";
import { toast } from "sonner";

import type { PluginView, SettingSchema } from "@/api/admin/ai/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { Notice } from "@/components/admin-ui/notice";
import { OptionSelect } from "@/components/admin-ui/option-select";
import { SearchInput } from "@/components/admin-ui/search-input";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Skeleton } from "@/components/ui/skeleton";
import { Tag } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import {
  changeProtocolVersion,
  filterProtocolOptions,
  protocolOptions,
  protocolVersions,
  selectProtocol,
  type ModelProtocol,
  type ProtocolOption,
} from "@/utils/admin/model-protocol";
import {
  describeAuth,
  findPluginVersion,
  latestVersion,
  metaSupportsKind,
} from "@/utils/admin/plugin";
import {
  initialSettingValues,
  settingFields,
  validateSettingValues,
  type SettingFormValue,
  type SettingFormValues,
} from "@/utils/admin/settings-form";

import type { LoadStatus } from "../../use-admin";
import { KIND_ORDER, KIND_STYLE } from "../kind";
import { SettingFields } from "../setting-fields";

const MODE_LABEL = { sync: "同步", async: "异步" } as const;

/** 能力分段旁的一句话说明 */
const KIND_DESC: Record<string, string> = {
  text: "对话、写作",
  image: "文生图、图生图",
  video: "文生、图生视频",
  audio: "语音合成",
  agent: "画布 Agent 的对话大模型",
};

/**
 * 能力与协议选择器（模型弹窗“基本信息”页与批量设置共用）：
 * ① 模型能力分段控件；② 可搜索的调用协议列表（已停用的插件置灰、写明原因）；
 * ③ 右侧“当前选择”：插件描述、鉴权、同步 / 异步与路径、版本下拉、协议设置项表单。
 * 选 Agent 时隐藏协议列表，显示固定说明。规则（切能力、切协议、切版本时设置项怎么整理）在 utils/admin/model-protocol。
 * 切能力由宿主处理（模型弹窗里要重置能力与定价、已保存的模型要先确认），选择器只通知。
 * @param kind 当前能力；没选为空串
 * @param protocol 当前协议；没选为 null
 * @param plugins 全部插件（带全部版本和 meta）
 * @param pluginsStatus 插件清单的加载状态；没加载好时不让切能力（切能力要按插件声明判断已选协议是否保留）
 * @param kindError 能力的校验问题（后端返回）
 * @param protocolError 协议的校验问题（后端返回）
 * @param onKindChange 点了另一个能力
 * @param onProtocolChange 协议变了（选中、换版本、改设置项）
 */
export function ProtocolPicker({
  idPrefix = "model",
  kind,
  protocol,
  plugins,
  pluginsStatus,
  kindError,
  protocolError,
  onKindChange,
  onProtocolChange,
}: {
  idPrefix?: string;
  kind: string;
  protocol: ModelProtocol | null;
  plugins: readonly PluginView[];
  pluginsStatus: LoadStatus;
  kindError?: string;
  protocolError?: string;
  onKindChange: (kind: string) => void;
  onProtocolChange: (protocol: ModelProtocol) => void;
}) {
  const ready = pluginsStatus === "ready";
  return (
    <div className="space-y-5">
      <FormField size="default" label="模型能力" required error={kindError}>
        <Segmented
          id={`${idPrefix}-kind`}
          aria-label="模型能力"
          className="w-fit max-w-full flex-wrap"
        >
          {KIND_ORDER.map((item) => {
            const Icon = KIND_STYLE[item].icon;
            return (
              <SegmentedItem
                key={item}
                slideId={`${idPrefix}-kind-seg`}
                active={item === kind}
                disabled={!ready}
                title={`${MODEL_KIND_LABEL[item]}：${KIND_DESC[item]}`}
                className="flex items-center gap-1.5 text-sm"
                onClick={() => item !== kind && onKindChange(item)}
              >
                <Icon className={cn("size-4", KIND_STYLE[item].text)} />
                {MODEL_KIND_LABEL[item]}
              </SegmentedItem>
            );
          })}
        </Segmented>
      </FormField>

      <div id={`${idPrefix}-protocol`} tabIndex={-1} className="space-y-2 outline-none">
        {!kind ? (
          <Notice tone="info">先选择模型能力，再从协议列表里选调用协议。</Notice>
        ) : kind === "agent" ? (
          <Notice tone="info" title="Agent 模型不需要选择协议">
            由宿主的 LLM 网关直接用渠道 API Key 作 Bearer，请求渠道的 OpenAI
            兼容接口，渠道的自定义请求头也会带上。
          </Notice>
        ) : pluginsStatus === "error" ? (
          <Notice tone="danger" title="协议列表加载失败">
            刷新页面后再试。
          </Notice>
        ) : !ready ? (
          <div className="space-y-2" role="status" aria-busy="true" aria-label="协议列表加载中">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-32 w-full" />
          </div>
        ) : (
          <FormField size="default" label="调用协议" required error={protocolError}>
            <ProtocolList
              key={kind}
              idPrefix={idPrefix}
              kind={kind}
              protocol={protocol}
              plugins={plugins}
              onProtocolChange={onProtocolChange}
            />
          </FormField>
        )}
      </div>
    </div>
  );
}

/** 协议列表 + 当前选择详情；搜索词放在这里，切能力时随 key 重置 */
function ProtocolList({
  idPrefix,
  kind,
  protocol,
  plugins,
  onProtocolChange,
}: {
  idPrefix: string;
  kind: string;
  protocol: ModelProtocol | null;
  plugins: readonly PluginView[];
  onProtocolChange: (protocol: ModelProtocol) => void;
}) {
  const [query, setQuery] = useState("");
  const options = useMemo(() => protocolOptions(plugins, kind), [plugins, kind]);
  const shown = filterProtocolOptions(options, query);

  const pick = (option: ProtocolOption) => {
    if (!option.enabled || protocol?.plugin === option.key) return;
    const next = selectProtocol(plugins, option.key, kind);
    if (next) onProtocolChange(next);
  };

  return (
    <div className="space-y-2">
      <SearchInput
        aria-label="搜索协议"
        placeholder="搜索协议名称、厂商或请求路径"
        autoComplete="off"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
        <div
          role="radiogroup"
          aria-label="调用协议"
          className="flex max-h-96 flex-col gap-2 overflow-y-auto pr-1"
        >
          {shown.length === 0 && (
            <p className="text-muted-foreground rounded-lg border border-dashed p-4 text-sm">
              {options.length === 0
                ? `没有支持“${MODEL_KIND_LABEL[kind] ?? kind}”的协议，请先让 super_admin 上传相应的协议插件。`
                : "没有匹配的协议，换个关键词试试。"}
            </p>
          )}
          {shown.map((option) => (
            <ProtocolRow
              key={option.key}
              option={option}
              selected={protocol?.plugin === option.key}
              onPick={() => pick(option)}
            />
          ))}
        </div>
        <div className="bg-muted/30 min-w-0 space-y-3 rounded-lg border p-3">
          {protocol ? (
            <ProtocolDetail
              idPrefix={idPrefix}
              kind={kind}
              protocol={protocol}
              plugins={plugins}
              onProtocolChange={onProtocolChange}
            />
          ) : (
            <p className="text-muted-foreground text-sm">当前选择：从左侧选一个协议查看详情。</p>
          )}
        </div>
      </div>
    </div>
  );
}

/** 协议列表的一行：名称、“METHOD path”、厂商、同步 / 异步，右侧单选；停用的插件置灰不可选 */
function ProtocolRow({
  option,
  selected,
  onPick,
}: {
  option: ProtocolOption;
  selected: boolean;
  onPick: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      aria-disabled={!option.enabled}
      data-selected={selected || undefined}
      className={cn(
        "flex items-start gap-3 rounded-lg border p-3 text-left transition",
        "data-selected:border-foreground/60 data-selected:bg-accent",
        option.enabled ? "hover:bg-accent/50" : "cursor-not-allowed opacity-60",
      )}
      onClick={onPick}
    >
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{option.name}</span>
        <span className="text-muted-foreground mt-0.5 block truncate font-mono text-xs">
          {option.method} {option.path || "—"}
        </span>
        {option.disabledReason && (
          <span className="mt-0.5 block text-xs text-amber-700 dark:text-amber-400">
            {option.disabledReason}
          </span>
        )}
      </span>
      <span className="text-muted-foreground max-w-24 shrink-0 truncate text-xs">
        {option.vendor || "—"}
      </span>
      {option.mode && <Tag className="shrink-0">{MODE_LABEL[option.mode]}</Tag>}
      <span
        aria-hidden="true"
        className={cn(
          "mt-0.5 grid size-4 shrink-0 place-items-center rounded-full border",
          selected ? "border-foreground" : "border-input",
        )}
      >
        {selected && <span className="bg-foreground size-2 rounded-full" />}
      </span>
    </button>
  );
}

/**
 * 右侧“当前选择”：插件描述、鉴权、同步 / 异步与路径、版本下拉、协议设置项。
 * 保存下来的协议可能已经失效（插件被删、已停用、版本被删、版本不支持这个能力），这里就地说明。
 */
function ProtocolDetail({
  idPrefix,
  kind,
  protocol,
  plugins,
  onProtocolChange,
}: {
  idPrefix: string;
  kind: string;
  protocol: ModelProtocol;
  plugins: readonly PluginView[];
  onProtocolChange: (protocol: ModelProtocol) => void;
}) {
  const plugin = plugins.find((item) => item.key === protocol.plugin);
  const latest = latestVersion(plugin);
  const found = protocol.version
    ? findPluginVersion(plugins, protocol.plugin, { version: protocol.version })
    : latest;
  const meta = found?.meta ?? null;
  const endpoint = meta?.endpoints?.[kind];
  const supports = !!meta && metaSupportsKind(meta, kind);
  const versions = protocolVersions(plugin, kind, found?.version ?? protocol.version);

  const changeVersion = (next: string) => {
    if (next === protocol.version) return;
    const result = changeProtocolVersion(plugins, protocol, next);
    onProtocolChange(result.protocol);
    if (result.dropped.length > 0)
      toast.warning(`新版本不再声明协议设置项：${result.dropped.join("、")}，已丢弃`);
  };

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <b className="text-sm">{plugin?.name ?? protocol.plugin}</b>
        <Tag mono>{protocol.plugin}</Tag>
      </div>
      {!plugin && (
        <Notice tone="danger">协议插件 {protocol.plugin} 不存在，请从左侧重新选择协议。</Notice>
      )}
      {plugin && !plugin.enabled && (
        <Notice tone="warning">插件已停用，这个模型无法启用，请换一个协议。</Notice>
      )}
      {plugin && !found && (
        <Notice tone="danger">固定的版本 {protocol.version} 已不存在，请换一个版本。</Notice>
      )}
      {found && !meta && <Notice tone="danger">这个版本的 meta 无法解析。</Notice>}
      {meta && !supports && (
        <Notice tone="danger">
          这个版本不支持{MODEL_KIND_LABEL[kind] ?? kind}，请换一个版本或协议。
        </Notice>
      )}
      {meta?.description && <p className="text-muted-foreground text-sm">{meta.description}</p>}
      {meta && (
        <div className="flex flex-wrap items-center gap-1.5">
          <Tag tone="info">{describeAuth(meta.auth)}</Tag>
          {endpoint?.mode === "sync" || endpoint?.mode === "async" ? (
            <Tag>{MODE_LABEL[endpoint.mode]}</Tag>
          ) : null}
          {endpoint && (
            <Tag mono>
              {(endpoint.method || "POST").toUpperCase()} {endpoint.path || "—"}
            </Tag>
          )}
        </div>
      )}
      {meta?.auth?.type === "custom" && (
        <Notice tone="warning">
          该协议会读取<b>渠道的 API Key（和 Secret Key）</b>自行签名；渠道里不需要再开任何开关。
        </Notice>
      )}
      {plugin && (
        <FormField
          label="版本"
          htmlFor={`${idPrefix}-protocol-version`}
          hint="默认最新版本，保存后固定；批量升级在“插件”页进行。"
        >
          <OptionSelect
            id={`${idPrefix}-protocol-version`}
            size="sm"
            value={found?.version ?? protocol.version}
            onValueChange={changeVersion}
            options={versions.map((version) => ({
              value: version,
              label: `v${version}${
                version === latest?.version
                  ? "（最新）"
                  : plugin.versions.some((item) => item.version === version)
                    ? ""
                    : "（已被删除）"
              }`,
            }))}
          />
        </FormField>
      )}
      {meta && (
        <ProtocolSettings
          key={`${protocol.plugin}@${found?.version ?? protocol.version}`}
          idPrefix={`${idPrefix}-protocol`}
          schema={meta.channelSettings}
          settings={protocol.settings}
          onChange={(settings) => onProtocolChange({ ...protocol, settings })}
        />
      )}
    </>
  );
}

/**
 * 协议设置项表单（原“渠道设置项”，现在跟着模型走）：按所选版本的 channelSettings 渲染。
 * 输入框里的原始文本放在本组件自己的 state 里（数字写到一半、必填项暂时清空都不能被正文反向覆盖），
 * 每次改动把校验通过的取值回写给协议；必填项没填就地标红。切协议 / 版本时用 key 重置。
 */
function ProtocolSettings({
  idPrefix,
  schema,
  settings,
  onChange,
}: {
  idPrefix: string;
  schema: SettingSchema | null | undefined;
  settings: Record<string, unknown>;
  onChange: (settings: Record<string, unknown>) => void;
}) {
  const fields = useMemo(() => settingFields(schema), [schema]);
  const [form, setForm] = useState<SettingFormValues>(() => initialSettingValues(fields, settings));
  if (fields.length === 0) return null;
  const { errors } = validateSettingValues(fields, form);

  const change = (name: string, value: SettingFormValue) => {
    const next = { ...form, [name]: value };
    setForm(next);
    onChange(validateSettingValues(fields, next).values);
  };

  return (
    <div className="space-y-2">
      <p className="text-xs font-medium">
        协议设置 <span className="text-muted-foreground font-normal">（跟着模型走）</span>
      </p>
      <SettingFields
        idPrefix={idPrefix}
        fields={fields}
        values={form}
        errors={errors}
        onChange={change}
      />
    </div>
  );
}
