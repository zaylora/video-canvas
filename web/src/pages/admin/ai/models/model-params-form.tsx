import { useState } from "react";
import { ChevronRight } from "lucide-react";

import type { ConfigIssue } from "@/api/admin/ai/type.d";
import type { Capabilities, GenerationOp, ParamField, RefKind, RefSpec } from "@/api/model/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { ToggleChip } from "@/components/admin-ui/toggle-chip";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  checkDeadline,
  readModelKind,
  readModelString,
  withModelField,
} from "@/utils/admin/model-body";
import { defaultCapabilities } from "@/utils/admin/model-template";

import {
  AddParamRow,
  ContextEditor,
  LIMITS,
  OPS_OF_KIND,
  OpsEditor,
  paramSetOps,
  ParamRowEditor,
  RefCardEditor,
} from "./capability-editors";
import { JsonFieldEditor } from "./json-field-editor";
import { issueFor } from "./model-fields";

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

const DEADLINES: Array<[string, string]> = [
  ["5m", "5 分钟"],
  ["10m", "10 分钟"],
  ["20m", "20 分钟"],
  ["30m", "30 分钟"],
  ["60m", "60 分钟"],
];

const REF_OFF: RefSpec = { on: false, max: 0, max_mb: 0 };

/** 读正文里的 capabilities；缺失的部分补成空值，表单可以直接渲染 */
function readCaps(body: Record<string, unknown>): Capabilities {
  const raw = body.capabilities;
  const caps = (
    raw && typeof raw === "object" && !Array.isArray(raw) ? raw : {}
  ) as Partial<Capabilities>;
  return {
    ...caps,
    refs: { image: REF_OFF, video: REF_OFF, audio: REF_OFF, ...caps.refs },
    prompt: caps.prompt ?? { max_length: 0 },
  };
}

/**
 * 能力与参数页签：任务超时，以及模型能力 capabilities 的结构化表单——生成方式、上下文（文本）、
 * 参考素材、提示词上限、生成参数（每个参数一行）、固定系统提示（文本）；另有固定参数（params）。
 * 所有控件都读写正文里的 capabilities，「用 JSON 编辑」折叠区和它是同一份数据。右侧的模拟节点随它实时变化。
 * @param epoch 正文被整体替换的计数，变化时 JSON 编辑框重新挂载
 */
export function ModelParamsForm({
  body,
  issues,
  epoch,
  onChange,
}: {
  body: Record<string, unknown>;
  issues: ConfigIssue[];
  epoch: number;
  onChange: (mutate: BodyMutator) => void;
}) {
  const [capsNonce, setCapsNonce] = useState(0);
  const deadline = readModelString(body, "deadline");
  const custom = !!deadline && !DEADLINES.some(([value]) => value === deadline);
  const deadlineError = deadline ? checkDeadline(deadline) : null;
  const kind = readModelKind(body);
  const caps = readCaps(body);
  const params = caps.params ?? {};
  const isMedia = kind === "video" || kind === "image";
  const setField = (field: string, value: unknown) =>
    onChange((body) => withModelField(body, field, value));
  /** 图形化改了 capabilities：写回正文，并让 JSON 编辑框重新挂载显示最新内容 */
  const setCaps = (next: Capabilities) => {
    setField("capabilities", next);
    setCapsNonce((value) => value + 1);
  };
  const patch = (partial: Partial<Capabilities>) => setCaps({ ...caps, ...partial });
  const setRef = (refKind: RefKind, value: RefSpec) =>
    patch({ refs: { ...caps.refs, [refKind]: value } });
  const setParams = (next: Record<string, ParamField>) => patch({ params: next });
  const refKinds: RefKind[] = kind === "video" ? ["image", "audio", "video"] : ["image"];
  const promptIssue = issueFor(issues, "capabilities.prompt");
  const promptOut =
    caps.prompt.max_length < LIMITS.promptLength[0] ||
    caps.prompt.max_length > LIMITS.promptLength[1];

  const isAgent = kind === "agent";

  return (
    <>
      {!isAgent && (
        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>任务设置</FormSectionTitle>
          </FormSectionHeader>
          <FormField
            size="default"
            label="任务超时"
            htmlFor="model-deadline"
            error={deadlineError ?? issueFor(issues, "deadline")}
            hint="超过这个时间还没出结果，任务判定失败并退还积分。"
          >
            <div className="flex flex-wrap items-center gap-2" id="model-deadline">
              {DEADLINES.map(([value, label]) => (
                <ToggleChip
                  key={value}
                  pressed={deadline === value}
                  onClick={() => setField("deadline", value)}
                >
                  {label}
                </ToggleChip>
              ))}
              <Input
                aria-label="自定义超时"
                placeholder="自定义，如 90s"
                className="h-8 w-36 font-mono text-xs"
                value={custom ? deadline : ""}
                onChange={(event) => setField("deadline", event.target.value)}
              />
            </div>
          </FormField>
        </FormSection>
      )}

      {OPS_OF_KIND[kind] && (
        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>生成方式</FormSectionTitle>
            <FormSectionDescription>
              用户在画布节点上可以切换的方式，至少保留一种。
            </FormSectionDescription>
          </FormSectionHeader>
          <OpsEditor
            kind={kind}
            ops={(caps.ops ?? []) as GenerationOp[]}
            onChange={(ops) => patch({ ops })}
          />
          {issueFor(issues, "capabilities.ops") && (
            <p className="text-destructive mt-2 text-xs">{issueFor(issues, "capabilities.ops")}</p>
          )}
        </FormSection>
      )}

      {(kind === "text" || isAgent) && (
        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>上下文能力</FormSectionTitle>
            <FormSectionDescription>
              超出允许范围时标红；最大输出会作为每次请求的 max_tokens。
              {isAgent && "Agent 会把对话历史带进每次请求，窗口太小会频繁省略较早的工具结果。"}
            </FormSectionDescription>
          </FormSectionHeader>
          <ContextEditor value={caps.context} onChange={(context) => patch({ context })} />
          {isAgent && (
            <FormField
              size="default"
              className="mt-4"
              label="看图"
              error={issueFor(issues, "capabilities.vision")}
              hint="上游模型支持图片输入时打开：Agent 才能查看画布上的图片，判断画面是否符合要求。不能看图的模型拿不到看图工具。"
            >
              <ToggleChip pressed={!!caps.vision} onClick={() => patch({ vision: !caps.vision })}>
                能看图
              </ToggleChip>
            </FormField>
          )}
          {issueFor(issues, "capabilities.context") && (
            <p className="text-destructive mt-2 text-xs">
              {issueFor(issues, "capabilities.context")}
            </p>
          )}
        </FormSection>
      )}

      {isMedia && (
        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>参考素材</FormSectionTitle>
            <FormSectionDescription>
              用户可以连到这个节点上的素材；关闭的类型即使生成方式允许也不接收。
            </FormSectionDescription>
          </FormSectionHeader>
          <div className="grid gap-3 sm:grid-cols-2">
            {refKinds.map((refKind) => (
              <RefCardEditor
                key={refKind}
                kind={refKind}
                value={caps.refs[refKind]}
                onChange={(value) => setRef(refKind, value)}
              />
            ))}
          </div>
          {(["refs", ...refKinds.map((k) => `refs.${k}`)] as const).map((path) => {
            const message = issueFor(issues, `capabilities.${path}`);
            return message ? (
              <p key={path} className="text-destructive mt-2 text-xs">
                {message}
              </p>
            ) : null;
          })}
        </FormSection>
      )}

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>提示词</FormSectionTitle>
        </FormSectionHeader>
        <FormField
          size="default"
          label={isAgent ? "单条消息字数上限" : "字数上限"}
          htmlFor="model-prompt-length"
          error={promptIssue}
          hint={
            isAgent
              ? `用户发给 Agent 的单条消息最多多少字；允许范围 ${LIMITS.promptLength[0]} – ${LIMITS.promptLength[1]}。`
              : `画布输入框右下角显示 0 / 上限；允许范围 ${LIMITS.promptLength[0]} – ${LIMITS.promptLength[1]}。`
          }
        >
          <Input
            id="model-prompt-length"
            type="number"
            className="w-36 tabular-nums"
            aria-invalid={promptOut}
            value={caps.prompt.max_length || ""}
            onChange={(event) =>
              patch({ prompt: { max_length: Math.trunc(Number(event.target.value)) || 0 } })
            }
          />
        </FormField>
      </FormSection>

      {!isAgent && (
        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>生成参数</FormSectionTitle>
            <FormSectionDescription>
              每个参数会作为选项出现在画布节点里，书写顺序就是显示顺序；参数名会作为任务输入的键传给插件，
              要和插件约定的名字对应。
            </FormSectionDescription>
          </FormSectionHeader>
          <div id="model-capabilities-params" className="space-y-3">
            {Object.keys(params).length === 0 && (
              <p className="text-muted-foreground rounded-md border border-dashed p-3 text-center text-xs">
                {kind === "text" ? "文本模型默认没有生成参数。" : "还没有生成参数。"}
              </p>
            )}
            {Object.entries(params).map(([name, field], index, all) => (
              <ParamRowEditor
                key={name}
                name={name}
                field={field}
                first={index === 0}
                last={index === all.length - 1}
                error={issueFor(issues, `capabilities.params.${name}`)}
                onChange={(next) => setParams(paramSetOps.patch(params, name, next))}
                onMove={(direction) => setParams(paramSetOps.move(params, name, direction))}
                onRemove={() => setParams(paramSetOps.remove(params, name))}
              />
            ))}
            <AddParamRow
              existing={Object.keys(params)}
              onAdd={(name, field) => setParams(paramSetOps.add(params, name, field))}
            />
            {issueFor(issues, "capabilities.params") && (
              <p className="text-destructive text-xs">{issueFor(issues, "capabilities.params")}</p>
            )}
          </div>
        </FormSection>
      )}

      {kind === "text" && (
        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>固定系统提示</FormSectionTitle>
            <FormSectionDescription>
              每次请求都会带上，用户看不到，也不会下发给画布。
            </FormSectionDescription>
          </FormSectionHeader>
          <Textarea
            id="model-system"
            rows={4}
            aria-label="固定系统提示"
            placeholder="例如：你是一名资深分镜师，回答使用中文。"
            value={caps.system ?? ""}
            onChange={(event) => patch({ system: event.target.value || undefined })}
          />
          {issueFor(issues, "capabilities.system") && (
            <p className="text-destructive mt-2 text-xs">
              {issueFor(issues, "capabilities.system")}
            </p>
          )}
        </FormSection>
      )}

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>{isAgent ? "高级" : "固定参数"}</FormSectionTitle>
          {!isAgent && (
            <FormSectionDescription>
              每次请求都会带上，用户看不到；由插件解释。
            </FormSectionDescription>
          )}
        </FormSectionHeader>
        {!isAgent && (
          <JsonFieldEditor
            key={`params-${epoch}`}
            id="model-params"
            label="params"
            value={body.params}
            issue={issueFor(issues, "params")}
            rows={5}
            onValid={(value) => setField("params", value)}
          />
        )}
        <details className="group mt-4 rounded-lg border" open={!!issueFor(issues, "capabilities")}>
          <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-sm font-medium">
            <ChevronRight className="size-4 transition group-open:rotate-90" />
            用 JSON 编辑全部能力
            <span className="text-muted-foreground text-xs font-normal">
              capabilities，和上面的表单是同一份数据
            </span>
          </summary>
          <div className="border-t p-3">
            <JsonFieldEditor
              key={`capabilities-${epoch}-${capsNonce}`}
              id="model-capabilities"
              label="capabilities"
              value={body.capabilities}
              issue={issueFor(issues, "capabilities")}
              rows={16}
              actions={
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={() => setCaps(defaultCapabilities(kind))}
                >
                  恢复{kind ? "该种类的" : ""}默认能力
                </Button>
              }
              onValid={(value) => setField("capabilities", value)}
            />
          </div>
        </details>
      </FormSection>
    </>
  );
}
