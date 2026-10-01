import { useState } from "react";
import { ChevronRight } from "lucide-react";

import type { ConfigIssue } from "@/api/admin-ai/type";
import type { InputSchema } from "@/api/model/type";
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
import { checkDeadline, readModelString, withModelField } from "@/utils/admin/model-body";

import { MODEL_TEMPLATES } from "../templates";
import { JsonFieldEditor } from "./json-field-editor";
import { issueFor } from "./model-fields";
import { SchemaFieldEditor } from "./schema-field-editor";

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

const DEADLINES: Array<[string, string]> = [
  ["5m", "5 分钟"],
  ["10m", "10 分钟"],
  ["20m", "20 分钟"],
  ["30m", "30 分钟"],
  ["60m", "60 分钟"],
];

/**
 * 能力与参数页签：任务超时、参考素材与生成参数（input_schema 的图形化配置 + JSON）、固定参数（params）。
 * 右侧的模拟节点随 input_schema 实时变化。
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
  const [schemaNonce, setSchemaNonce] = useState(0);
  const deadline = readModelString(body, "deadline");
  const custom = !!deadline && !DEADLINES.some(([value]) => value === deadline);
  const deadlineError = deadline ? checkDeadline(deadline) : null;
  const setField = (field: string, value: unknown) =>
    onChange((body) => withModelField(body, field, value));
  const schema = (
    body.input_schema && typeof body.input_schema === "object" && !Array.isArray(body.input_schema)
      ? body.input_schema
      : {}
  ) as InputSchema;
  /** 图形化改了 schema 之后，让 JSON 编辑框重新挂载显示最新内容 */
  const setSchema = (next: InputSchema) => {
    setField("input_schema", next);
    setSchemaNonce((value) => value + 1);
  };
  const insertTemplate = (id: "text" | "video") => {
    const template = MODEL_TEMPLATES.find((item) => item.id === id);
    if (!template) return;
    setField("input_schema", structuredClone(template.body.input_schema));
    setSchemaNonce((value) => value + 1);
  };

  return (
    <>
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

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>参考素材</FormSectionTitle>
          <FormSectionDescription>用户可以连到这个节点上的素材。</FormSectionDescription>
        </FormSectionHeader>
        <SchemaFieldEditor schema={schema} media onChange={setSchema} />
      </FormSection>

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>生成参数</FormSectionTitle>
          <FormSectionDescription>
            每个参数会作为选项出现在画布节点里，书写顺序就是显示顺序；右边的节点会立即变化。
          </FormSectionDescription>
        </FormSectionHeader>
        <SchemaFieldEditor schema={schema} media={false} onChange={setSchema} />
        <details className="group mt-3 rounded-lg border" open={!!issueFor(issues, "input_schema")}>
          <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-sm font-medium">
            <ChevronRight className="size-4 transition group-open:rotate-90" />
            用 JSON 编辑全部字段
            <span className="text-muted-foreground text-xs font-normal">
              增删字段、改选项、改端口
            </span>
          </summary>
          <div className="border-t p-3">
            <JsonFieldEditor
              key={`schema-${epoch}-${schemaNonce}`}
              id="model-input-schema"
              label="input_schema"
              value={body.input_schema}
              issue={issueFor(issues, "input_schema")}
              rows={14}
              actions={
                <>
                  <Button size="xs" variant="ghost" onClick={() => insertTemplate("text")}>
                    插入文本模板
                  </Button>
                  <Button size="xs" variant="ghost" onClick={() => insertTemplate("video")}>
                    插入视频模板
                  </Button>
                </>
              }
              onValid={(value) => setField("input_schema", value)}
            />
          </div>
        </details>
      </FormSection>

      <FormSection>
        <FormSectionHeader>
          <FormSectionTitle>固定参数</FormSectionTitle>
          <FormSectionDescription>
            每次请求都会带上，用户看不到；由插件解释。
          </FormSectionDescription>
        </FormSectionHeader>
        <JsonFieldEditor
          key={`params-${epoch}`}
          id="model-params"
          label="params"
          value={body.params}
          issue={issueFor(issues, "params")}
          rows={5}
          onValid={(value) => setField("params", value)}
        />
      </FormSection>
    </>
  );
}
