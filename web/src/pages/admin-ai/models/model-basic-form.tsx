import { ShieldCheck } from "lucide-react";
import { Link } from "react-router";

import type { ChannelView, ConfigIssue, PluginView } from "@/api/admin-ai/type";
import { ChoiceCard, ChoiceCardGroup } from "@/components/admin-ui/choice-card";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { Notice } from "@/components/admin-ui/notice";
import { Tag, toneClasses } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import {
  MODEL_KIND_LABEL,
  readModelChannel,
  readModelKind,
  readModelNumber,
  readModelString,
  suggestModelKey,
  withModelChannel,
  withModelField,
  withModelUpstream,
} from "@/utils/admin/model-body";
import type { ModelChannelInfo } from "@/utils/admin/model-channel";
import { channelMeta, channelSupportsKind } from "@/utils/admin/plugin";

import { KIND_ORDER, KIND_STYLE } from "../kind";
import { issueFor } from "./model-fields";

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

const KIND_DESC: Record<string, string> = {
  text: "对话、写作",
  image: "文生图、图生图",
  video: "文生、图生视频",
  audio: "语音合成",
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
  const pickChannel = (item: ChannelView) =>
    onChange((body) => {
      const next = withModelChannel(body, item.key);
      if (!next || channelSupportsKind(plugins, item, kind) !== false) return next;
      const fallback = KIND_ORDER.find((k) => channelSupportsKind(plugins, item, k) !== false);
      return fallback ? withModelField(next, "kind", fallback) : next;
    });
  const setField = (field: string, value: unknown) =>
    onChange((body) => withModelField(body, field, value));
  const keyMissing = (item: ChannelView) => {
    const auth = channelMeta(plugins, item)?.auth?.type ?? "none";
    return !item.secret_set && auth !== "none";
  };
  const hint = readModelString(body, "hint");

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
        </div>
      </FormSection>

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>能力与渠道</FormSectionTitle>
          <FormSectionDescription>
            新建时选定能力与渠道，选定渠道后它不支持的能力会置灰；保存后不能再改。
          </FormSectionDescription>
        </FormSectionHeader>
        <div className="space-y-5">
          <FormField size="default" label="模型能力" required error={issueFor(issues, "kind")}>
            <ChoiceCardGroup
              id="model-kind"
              aria-label="模型能力"
              className="grid-cols-2 sm:grid-cols-4"
            >
              {KIND_ORDER.map((item) => {
                const Icon = KIND_STYLE[item].icon;
                return (
                  <ChoiceCard
                    key={item}
                    selected={item === kind}
                    disabled={!isNew || kindUnsupported(item)}
                    className={cn(
                      "items-center gap-2.5",
                      item === kind &&
                        cn(
                          toneClasses[KIND_STYLE[item].tone],
                          "data-selected:bg-transparent ring-1 ring-current",
                        ),
                    )}
                    onClick={() => setField("kind", item)}
                  >
                    <Icon className="size-5 shrink-0" />
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
                      disabled={!isNew}
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
              这个渠道的插件版本不支持{MODEL_KIND_LABEL[kind] ?? kind}
              ，发布会被拒绝。
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
            hint="在模型选择器里显示在名称下面，可留空。"
          >
            <Textarea
              id="model-hint"
              rows={3}
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
        </div>
      </FormSection>
    </>
  );
}
