import { useState } from "react";
import { ShieldCheck } from "lucide-react";
import { Link } from "react-router";

import type { ChannelView, ConfigIssue, PluginView } from "@/api/admin/ai/type.d";
import { ChoiceCard, ChoiceCardGroup } from "@/components/admin-ui/choice-card";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { TagInput } from "@/components/admin-ui/tag-input";
import { VendorPicker } from "@/components/admin-ui/vendor-picker";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useRetained } from "@/hooks/use-retained";
import { cn } from "@/lib/utils";
import {
  MODEL_HINT_MAX,
  MODEL_KIND_LABEL,
  MODEL_TAG_MAX_LENGTH,
  MODEL_TAGS_MAX,
  readModelChannel,
  readModelKind,
  readModelNumber,
  readModelString,
  readModelStrings,
  suggestModelKey,
  withModelChannel,
  withModelField,
  withModelUpstream,
} from "@/utils/admin/model-body";
import type { ModelChannelInfo } from "@/utils/admin/model-channel";
import { defaultCapabilities, defaultPricing } from "@/utils/admin/model-template";
import { channelMeta, channelSupportsKind } from "@/utils/admin/plugin";

import { KIND_ORDER, KIND_STYLE } from "../kind";
import { issueFor } from "./model-fields";

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

/** 新建时换种类：能力与定价跟着种类重置成该种类的默认值（生成方式、素材、参数、计费方式都不通用） */
function withKind(body: Record<string, unknown>, kind: string) {
  const next = withModelField(body, "kind", kind);
  const withCaps = next && withModelField(next, "capabilities", defaultCapabilities(kind));
  return withCaps && withModelField(withCaps, "pricing", defaultPricing(kind));
}

const KIND_DESC: Record<string, string> = {
  text: "对话、写作",
  image: "文生图、图生图",
  video: "文生、图生视频",
  audio: "语音合成",
  agent: "画布 Agent 的对话大模型",
};

/** 基本信息页签：模型身份、能力与渠道、展示信息。所有修改都回写 JSON 正文并保持键顺序 */
export function ModelBasicForm({
  body,
  isNew,
  channels,
  plugins,
  channelsReady,
  info,
  issues,
  onChange,
}: {
  body: Record<string, unknown>;
  isNew: boolean;
  channels: ChannelView[];
  plugins: PluginView[];
  channelsReady: boolean;
  info: ModelChannelInfo;
  issues: ConfigIssue[];
  onChange: (mutate: BodyMutator) => void;
}) {
  const kind = readModelKind(body);
  const { channel: channelKey, upstreamModel } = readModelChannel(body);
  const current = channels.find((item) => item.key === channelKey);
  // 渠道只按启用状态过滤；不支持的能力由能力卡片置灰，选了渠道后能力自动跟随
  const listed = channels.filter(
    (item) =>
      item.key === channelKey ||
      (item.enabled && plugins.find((p) => p.key === item.plugin_key)?.enabled !== false),
  );
  const kindUnsupported = (item: string) =>
    !!current && item !== kind && channelSupportsKind(plugins, current, item) === false;
  // 换能力会把能力与参数、定价重置成新种类的默认值；已保存的模型先确认，免得误点清掉配置
  const [pendingKind, setPendingKind] = useState<{ kind: string; channel?: string } | null>(null);
  // 关闭时 pendingKind 置空，确认框文案留到退出动画播完
  const shownKind = useRetained(pendingKind);
  const applyKind = (nextKind: string, nextChannel?: string) =>
    onChange((body) => {
      const next = nextChannel ? withModelChannel(body, nextChannel) : body;
      return next && withKind(next, nextKind);
    });
  const requestKind = (nextKind: string, nextChannel?: string) => {
    if (nextKind === kind) return;
    if (isNew) applyKind(nextKind, nextChannel);
    else setPendingKind({ kind: nextKind, channel: nextChannel });
  };
  const pickChannel = (item: ChannelView) => {
    if (item.key === channelKey) return;
    // 新渠道支持当前能力就只换渠道；不支持时能力跟着换成它支持的第一种
    if (channelSupportsKind(plugins, item, kind) !== false) {
      onChange((body) => withModelChannel(body, item.key));
      return;
    }
    const fallback = KIND_ORDER.find((k) => channelSupportsKind(plugins, item, k) !== false);
    if (fallback) requestKind(fallback, item.key);
    else onChange((body) => withModelChannel(body, item.key));
  };
  const setField = (field: string, value: unknown) =>
    onChange((body) => withModelField(body, field, value));
  const keyMissing = (item: ChannelView) => {
    const auth = channelMeta(plugins, item)?.auth?.type ?? "none";
    return !item.secret_set && auth !== "none";
  };
  const hint = readModelString(body, "hint");
  const tags = readModelStrings(body, "tags");

  return (
    <>
      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>模型身份</FormSectionTitle>
          <FormSectionDescription>区分产品侧的展示标识与上游实际调用的 ID。</FormSectionDescription>
        </FormSectionHeader>
        <div className="grid gap-5 md:grid-cols-2">
          <FormField
            size="default"
            label="产品模型标识"
            htmlFor="model-key"
            required
            error={issueFor(issues, "key")}
            tip="画布、任务与计费都用它引用这个模型，创建后不能修改。"
            hint={isNew ? "小写字母、数字、连字符，保存后不能修改。" : undefined}
          >
            <div className="flex gap-2">
              <Input
                id="model-key"
                className="font-mono"
                placeholder="例如 veo-4"
                value={readModelString(body, "key")}
                disabled={!isNew}
                aria-invalid={!!issueFor(issues, "key")}
                onChange={(event) => setField("key", event.target.value)}
              />
              {isNew && upstreamModel && !readModelString(body, "key") && (
                <Button
                  variant="outline"
                  onClick={() => setField("key", suggestModelKey(upstreamModel))}
                >
                  按上游 ID 生成
                </Button>
              )}
            </div>
          </FormField>
          <FormField
            size="default"
            label="上游模型 ID"
            htmlFor="model-upstream"
            required
            error={issueFor(issues, "channels[0].upstream_model")}
            tip="插件把这个值作为 model 参数发给上游网关。"
          >
            <Input
              id="model-upstream"
              className="font-mono"
              placeholder="实际发给上游的模型名"
              value={upstreamModel}
              aria-invalid={!!issueFor(issues, "channels[0].upstream_model")}
              onChange={(event) => onChange((body) => withModelUpstream(body, event.target.value))}
            />
          </FormField>
          <FormField
            size="default"
            label="展示名称"
            htmlFor="model-label"
            required
            error={issueFor(issues, "label")}
          >
            <Input
              id="model-label"
              placeholder="用户在画布里看到的名字"
              value={readModelString(body, "label")}
              aria-invalid={!!issueFor(issues, "label")}
              onChange={(event) => setField("label", event.target.value)}
            />
          </FormField>
          <FormField
            size="default"
            label="模型 Logo"
            htmlFor="model-vendor"
            error={issueFor(issues, "vendor")}
            hint="选厂商后，模型选择器里显示该厂商的 logo；不选则显示名称首字。"
          >
            <VendorPicker
              id="model-vendor"
              value={readModelString(body, "vendor")}
              name={readModelString(body, "label")}
              seed={readModelString(body, "key")}
              onChange={(slug) => setField("vendor", slug || undefined)}
            />
          </FormField>
        </div>
      </FormSection>

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>能力与渠道</FormSectionTitle>
          <FormSectionDescription>
            能力与渠道都可以修改；所选渠道的插件不支持的能力会置灰。换能力会把能力与参数、定价重置成新能力的默认值。
          </FormSectionDescription>
        </FormSectionHeader>
        <div className="space-y-5">
          <FormField size="default" label="模型能力" required error={issueFor(issues, "kind")}>
            <ChoiceCardGroup
              id="model-kind"
              aria-label="模型能力"
              className="grid-cols-2 sm:grid-cols-5"
            >
              {KIND_ORDER.map((item) => {
                const Icon = KIND_STYLE[item].icon;
                return (
                  <ChoiceCard
                    key={item}
                    selected={item === kind}
                    disabled={kindUnsupported(item)}
                    title={
                      kindUnsupported(item)
                        ? item === "agent"
                          ? "Agent 模型要求渠道的插件使用 Bearer 鉴权"
                          : "所选渠道的插件不支持这个能力"
                        : undefined
                    }
                    className="items-center gap-2.5"
                    onClick={() => requestKind(item)}
                  >
                    <Icon className={cn("size-5 shrink-0", KIND_STYLE[item].text)} />
                    <span>
                      <span className="block text-sm font-medium">{MODEL_KIND_LABEL[item]}</span>
                      <span className="block text-[11px] opacity-70">{KIND_DESC[item]}</span>
                    </span>
                  </ChoiceCard>
                );
              })}
            </ChoiceCardGroup>
          </FormField>

          <FormField
            size="default"
            label="所属渠道"
            required
            error={issueFor(issues, "channels[0].channel") ?? issueFor(issues, "channels")}
          >
            {!channelsReady ? (
              <p className="text-muted-foreground text-sm">渠道加载中…</p>
            ) : listed.length === 0 ? (
              <Notice tone="warning">
                还没有可用的渠道，需要先到
                <Link to="/admin/ai/channels" className="mx-1 underline">
                  渠道页
                </Link>
                新建一个。
              </Notice>
            ) : (
              <ChoiceCardGroup id="model-channel" aria-label="所属渠道" className="sm:grid-cols-2">
                {listed.map((item) => {
                  const plugin = plugins.find((p) => p.key === item.plugin_key);
                  const mode = channelMeta(plugins, item)?.endpoints?.[kind]?.mode;
                  return (
                    <ChoiceCard
                      key={item.key}
                      indicator
                      selected={item.key === channelKey}
                      onClick={() => pickChannel(item)}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{item.name}</span>
                        <span className="text-muted-foreground mt-0.5 block truncate text-xs">
                          {plugin?.name ?? item.plugin_key} v{item.plugin_version}
                          {mode && ` · ${mode === "async" ? "异步" : "同步"}`}
                        </span>
                      </span>
                      {!item.enabled ? (
                        <Tag tone="danger">已停用</Tag>
                      ) : keyMissing(item) ? (
                        <Tag tone="warning">未设 Key</Tag>
                      ) : (
                        <Tag tone="success">
                          <ShieldCheck />
                          Key
                        </Tag>
                      )}
                    </ChoiceCard>
                  );
                })}
              </ChoiceCardGroup>
            )}
          </FormField>

          {info.channel && info.keyMissing && (
            <Notice
              tone="warning"
              title="这个渠道还没有设置 Key，模型无法发布"
              action={
                <Link
                  className="text-xs font-medium underline underline-offset-4"
                  to={`/admin/ai/channels?edit=${encodeURIComponent(info.channel.key)}`}
                >
                  去渠道页
                </Link>
              }
            />
          )}
          {info.supportsKind === false && (
            <Notice tone="danger">
              {kind === "agent"
                ? "Agent 模型要求渠道的插件使用 Bearer 鉴权，这个渠道不满足，发布会被拒绝。"
                : `这个渠道的插件版本不支持${MODEL_KIND_LABEL[kind] ?? kind}，发布会被拒绝。`}
            </Notice>
          )}
        </div>
      </FormSection>

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>展示信息</FormSectionTitle>
          <FormSectionDescription>
            显示在创作端的模型选择器里，不影响调用与计费。
          </FormSectionDescription>
        </FormSectionHeader>
        <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_10rem]">
          <FormField
            size="default"
            label="模型描述"
            htmlFor="model-hint"
            error={issueFor(issues, "hint")}
            hint={
              <span className="flex justify-between gap-2">
                <span>在模型选择器里显示在名称下面，可留空。</span>
                <span
                  className={cn(
                    "tabular-nums",
                    hint.length >= MODEL_HINT_MAX && "text-destructive",
                  )}
                >
                  {hint.length} / {MODEL_HINT_MAX}
                </span>
              </span>
            }
          >
            <Textarea
              id="model-hint"
              rows={3}
              maxLength={MODEL_HINT_MAX}
              placeholder="说明适用场景和注意事项"
              value={hint}
              onChange={(event) => setField("hint", event.target.value)}
            />
          </FormField>
          <FormField
            size="default"
            label="排序"
            htmlFor="model-sort"
            error={issueFor(issues, "sort")}
            hint="越小越靠前。"
          >
            <Input
              id="model-sort"
              type="number"
              className="tabular-nums"
              value={readModelNumber(body, "sort") ?? ""}
              onChange={(event) => {
                const value = Number(event.target.value);
                setField(
                  "sort",
                  event.target.value.trim() === "" || !Number.isFinite(value) ? 0 : value,
                );
              }}
            />
          </FormField>
          <FormField
            size="default"
            label="展示标签"
            htmlFor="model-tags"
            className="md:col-span-2"
            error={issueFor(issues, "tags")}
            hint={`显示在模型名称旁，如「推荐」「带音轨」。最多 ${MODEL_TAGS_MAX} 个，每个不超过 ${MODEL_TAG_MAX_LENGTH} 字。`}
          >
            <TagInput
              id="model-tags"
              value={tags}
              max={MODEL_TAGS_MAX}
              maxLength={MODEL_TAG_MAX_LENGTH}
              onChange={(next) => setField("tags", next.length ? next : undefined)}
            />
          </FormField>
        </div>
      </FormSection>

      <ConfirmDialog
        open={!!pendingKind}
        title={`改成${MODEL_KIND_LABEL[shownKind?.kind ?? ""] ?? shownKind?.kind ?? ""}模型？`}
        description={
          <>
            {shownKind?.channel && "新渠道不支持当前能力，需要一起换能力。"}
            「能力与参数」和「积分定价」会重置成
            {MODEL_KIND_LABEL[shownKind?.kind ?? ""] ?? shownKind?.kind}
            模型的默认值，当前的配置会被替换。改动保存为草稿，发布后才对用户生效。
          </>
        }
        confirmLabel="确认修改"
        onConfirm={() => {
          if (pendingKind) applyKind(pendingKind.kind, pendingKind.channel);
          setPendingKind(null);
        }}
        onCancel={() => setPendingKind(null)}
      />
    </>
  );
}
