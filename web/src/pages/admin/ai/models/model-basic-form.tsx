import { useState } from "react";
import { toast } from "sonner";

import type { ConfigIssue } from "@/api/admin/ai/type.d";
import type { Capabilities } from "@/api/model/type.d";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { Notice } from "@/components/admin-ui/notice";
import { TagInput } from "@/components/admin-ui/tag-input";
import { VendorPicker } from "@/components/admin-ui/vendor-picker";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useRetained } from "@/hooks/use-retained";
import { cn } from "@/lib/utils";
import {
  MODEL_HINT_MAX,
  MODEL_KIND_LABEL,
  MODEL_TAG_MAX_LENGTH,
  MODEL_TAGS_MAX,
  readModelApiFormat,
  readModelChannel,
  readModelKind,
  readModelNumber,
  readModelString,
  readModelStrings,
  withModelApiFormat,
  withModelField,
  withModelUpstream,
} from "@/utils/admin/model-body";
import {
  readModelProtocol,
  switchModelKind,
  withModelKind,
  withModelProtocol,
  type ModelProtocol,
} from "@/utils/admin/model-protocol";

import { issueFor } from "./model-fields";
import { ProtocolPicker } from "./protocol-picker";
import type { ModelCatalog } from "./use-model-workspace";

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

/**
 * 基本信息页签：模型身份、能力与协议、展示信息。
 * 协议挂在模型上，与渠道无关：换渠道不影响协议。所有修改都回写 JSON 正文并保持键顺序。
 */
export function ModelBasicForm({
  body,
  isNew,
  catalog,
  issues,
  warnings,
  onChange,
}: {
  body: Record<string, unknown>;
  isNew: boolean;
  catalog: ModelCatalog;
  issues: ConfigIssue[];
  warnings: ConfigIssue[];
  onChange: (mutate: BodyMutator) => void;
}) {
  const { plugins, pluginsStatus } = catalog;
  const kind = readModelKind(body);
  const protocol = readModelProtocol(body);
  const apiFormat = readModelApiFormat(body);
  const { upstreamModel } = readModelChannel(body);
  // 换能力会把能力与参数、定价重置成新种类的默认值；已有能力的模型先确认，免得误点清掉配置
  const [pendingKind, setPendingKind] = useState<string | null>(null);
  // 关闭时 pendingKind 置空，确认框文案留到退出动画播完
  const shownKind = useRetained(pendingKind);
  const applyKind = (nextKind: string) => {
    const { notice } = switchModelKind(plugins, protocol, nextKind);
    onChange((current) => withModelKind(current, nextKind, plugins)?.body ?? null);
    if (notice) toast.info(notice);
  };
  const requestKind = (nextKind: string) => {
    if (nextKind === kind) return;
    // 新建的、还没选过能力的（待配置）模型没有可丢的配置，直接换
    if (isNew || !kind) applyKind(nextKind);
    else setPendingKind(nextKind);
  };
  const setProtocol = (next: ModelProtocol) =>
    onChange((current) => withModelProtocol(current, next));
  const setField = (field: string, value: unknown) =>
    onChange((current) => withModelField(current, field, value));
  const channelError = issueFor(issues, "channels[0].channel");
  const channelWarning = issueFor(warnings, "channels[0].channel");
  const channelNote = channelError
    ? { tone: "danger" as const, text: channelError }
    : channelWarning
      ? { tone: "warning" as const, text: channelWarning }
      : null;
  const hint = readModelString(body, "hint");
  const tags = readModelStrings(body, "tags");

  return (
    <>
      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>模型身份</FormSectionTitle>
        </FormSectionHeader>
        {channelNote && (
          <Notice tone={channelNote.tone} title="所属渠道" className="mb-4">
            {channelNote.text}
          </Notice>
        )}
        <div className="grid gap-5 md:grid-cols-2">
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
            label="上游模型标识"
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
              onChange={(event) =>
                onChange((current) => withModelUpstream(current, event.target.value))
              }
            />
          </FormField>
          <FormField
            size="default"
            label="模型 Logo"
            htmlFor="model-vendor"
            error={issueFor(issues, "vendor")}
          >
            <VendorPicker
              id="model-vendor"
              value={readModelString(body, "vendor")}
              name={readModelString(body, "label")}
              seed={readModelString(body, "key")}
              onChange={(slug) => setField("vendor", slug || undefined)}
            />
          </FormField>
          {kind === "agent" && (
            <FormField
              size="default"
              label="接口格式"
              htmlFor="model-api-format"
              error={issueFor(issues, "channels[0].api_format")}
              hint={
                apiFormat === "gemini"
                  ? "请求 {base}/antigravity/v1beta/models/{上游模型}:streamGenerateContent?alt=sse；渠道地址已有 /antigravity 路径段时不重复添加。鉴权 x-goog-api-key 与 Authorization: Bearer。"
                  : "请求 {base}/v1/chat/completions，鉴权 Authorization: Bearer。"
              }
            >
              <Select
                value={apiFormat}
                onValueChange={(value) => {
                  if (value === "openai" || value === "gemini") {
                    if (
                      value === "openai" &&
                      (body.capabilities as Capabilities | undefined)?.video
                    )
                      toast.info("接口格式改成 OpenAI 兼容后，「看视频」已关闭");
                    onChange((current) => withModelApiFormat(current, value));
                  }
                }}
              >
                <SelectTrigger
                  id="model-api-format"
                  className="w-full"
                  aria-invalid={!!issueFor(issues, "channels[0].api_format")}
                >
                  <SelectValue>
                    {apiFormat === "gemini" ? "Gemini 原生" : "OpenAI 兼容"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent align="start" alignItemWithTrigger={false}>
                  <SelectItem value="openai">OpenAI 兼容</SelectItem>
                  <SelectItem value="gemini">Gemini 原生</SelectItem>
                </SelectContent>
              </Select>
            </FormField>
          )}
        </div>
      </FormSection>

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>能力与协议</FormSectionTitle>
          <FormSectionDescription>
            先选任务类型，再选对应的调用协议；更换后请核对参数与价格。
          </FormSectionDescription>
        </FormSectionHeader>
        <div className="space-y-4">
          <ProtocolPicker
            idPrefix="model"
            kind={kind}
            protocol={protocol}
            plugins={plugins}
            pluginsStatus={pluginsStatus}
            kindError={issueFor(issues, "kind")}
            protocolError={issueFor(issues, "channels[0].protocol")}
            onKindChange={requestKind}
            onProtocolChange={setProtocol}
          />
        </div>
      </FormSection>

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>展示信息</FormSectionTitle>
        </FormSectionHeader>
        <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_10rem]">
          <FormField
            size="default"
            label="模型描述"
            htmlFor="model-hint"
            error={issueFor(issues, "hint")}
            hint={
              <span className="flex justify-between gap-2">
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
            tip={isNew ? undefined : "创建后的顺序在渠道的“自定义排序”里调整。"}
          >
            <Input
              id="model-sort"
              type="number"
              className="tabular-nums"
              disabled={!isNew}
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
        title={`改成${MODEL_KIND_LABEL[shownKind ?? ""] ?? shownKind ?? ""}模型？`}
        description={
          <>
            「能力与参数」和「积分定价」会重置成
            {MODEL_KIND_LABEL[shownKind ?? ""] ?? shownKind}
            模型的默认值，当前的配置会被替换；已选的协议不支持新能力时也会被清空。保存后，已上线的模型立即生效。
          </>
        }
        confirmLabel="确认修改"
        onConfirm={() => {
          if (pendingKind) applyKind(pendingKind);
          setPendingKind(null);
        }}
        onCancel={() => setPendingKind(null)}
      />
    </>
  );
}
