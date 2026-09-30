import { useMemo, useState } from "react";
import { Link } from "react-router";
import { Eye } from "lucide-react";

import type { ChannelView, ConfigIssue, PluginView } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { channelsForKind } from "@/utils/admin/plugin";
import {
  checkDeadline,
  MODEL_KIND_LABEL,
  MODEL_KINDS,
  readModelBool,
  readModelChannel,
  readModelKind,
  readModelNumber,
  readModelString,
  suggestModelKey,
  withModelChannel,
  withModelField,
  withModelUpstream,
} from "@/utils/admin/model-body";
import { resolveModelChannel } from "@/utils/admin/model-channel";

import { FormField, NativeSelect, Notice, Tag } from "../shared";
import { MODEL_TEMPLATES } from "../templates";
import { JsonFieldEditor } from "./json-field-editor";

/** 校验问题的 path → 表单控件 id，用于点击问题时定位到字段 */
export const MODEL_FIELD_IDS: Record<string, string> = {
  key: "model-key",
  kind: "model-kind",
  label: "model-label",
  hint: "model-hint",
  credits: "model-credits",
  deadline: "model-deadline",
  sort: "model-sort",
  enabled: "model-enabled",
  channels: "model-channel",
  params: "model-params",
  input_schema: "model-input-schema",
};

/** path 对应的表单控件 id：取第一段（channels[0].upstream_model 单独映射） */
export function fieldIdForPath(path: string): string | null {
  if (path.startsWith("channels[0].upstream_model")) return "model-upstream";
  if (path.startsWith("channels")) return "model-channel";
  const head = path.split(/[.[]/)[0];
  return MODEL_FIELD_IDS[head] ?? null;
}

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

/** 后端校验问题里落在某个字段上的第一条说明 */
const issueFor = (issues: ConfigIssue[], path: string) =>
  issues.find(
    (issue) =>
      issue.path === path || issue.path.startsWith(`${path}.`) || issue.path.startsWith(`${path}[`),
  )?.message;

/** 画布表单预览：按 input_schema 的书写顺序渲染只读控件，让运营确认画布上会长什么样 */
function SchemaPreviewDialog({
  open,
  schema,
  onClose,
}: {
  open: boolean;
  schema: unknown;
  onClose: () => void;
}) {
  const fields = useMemo(
    () =>
      schema && typeof schema === "object" && !Array.isArray(schema)
        ? Object.entries(schema as Record<string, Record<string, unknown> | null>)
        : [],
    [schema],
  );
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>画布表单预览</DialogTitle>
          <DialogDescription>
            按 input_schema 的书写顺序，只读预览，与画布上的渲染顺序一致。
          </DialogDescription>
        </DialogHeader>
        {fields.length === 0 ? (
          <p className="text-muted-foreground text-sm">input_schema 里还没有字段。</p>
        ) : (
          <ul className="flex max-h-96 flex-col gap-3 overflow-y-auto">
            {fields.map(([name, spec]) => {
              const type = typeof spec?.type === "string" ? spec.type : "text";
              const options = Array.isArray(spec?.options)
                ? (spec.options as Array<{ value?: unknown; label?: string }>)
                : [];
              return (
                <li key={name} className="flex flex-col gap-1">
                  <span className="text-xs font-medium">
                    {typeof spec?.label === "string" ? spec.label : name}
                    {spec?.required === true && <span className="text-destructive">*</span>}
                    <span className="text-muted-foreground ml-2 font-mono font-normal">
                      {name} · {type}
                      {typeof spec?.port === "string" ? ` · 端口 ${spec.port}` : ""}
                    </span>
                  </span>
                  {type === "enum" ? (
                    <NativeSelect disabled aria-label={name}>
                      {options.map((option, index) => (
                        <option key={index}>{option.label ?? String(option.value)}</option>
                      ))}
                    </NativeSelect>
                  ) : type === "image" || type === "video" || type === "audio" ? (
                    <div className="text-muted-foreground rounded-lg border border-dashed p-3 text-center text-xs">
                      上传或连接 {type} 素材
                    </div>
                  ) : (
                    <Input
                      disabled
                      aria-label={name}
                      placeholder={type === "number" ? "数字" : "文本"}
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * 模型编辑器的“表单”视图：对 JSON 正文的一层读写，JSON 才是事实来源。
 * 通用字段（身份、渠道与上游模型、上架）有控件；params 与 input_schema 的形状由插件定义，
 * 宿主只存不解释，所以只提供 JSON 编辑框。所有修改都通过 onChange 回写正文并保持键顺序。
 * @param body 已解析的正文
 * @param issues 最近一次保存 / 校验的问题，就地显示在对应字段下
 * @param epoch 正文被整体替换的计数，变化时 JSON 编辑框重新挂载
 */
export function ModelForm({
  body,
  isNew,
  channels,
  plugins,
  channelsReady,
  issues,
  epoch,
  onChange,
}: {
  body: Record<string, unknown>;
  isNew: boolean;
  channels: ChannelView[];
  plugins: PluginView[];
  channelsReady: boolean;
  issues: ConfigIssue[];
  epoch: number;
  onChange: (mutate: BodyMutator) => void;
}) {
  const [schemaNonce, setSchemaNonce] = useState(0);
  const [previewOpen, setPreviewOpen] = useState(false);
  const kind = readModelKind(body);
  const { channel: channelKey, upstreamModel } = readModelChannel(body);
  const deadline = readModelString(body, "deadline");
  const deadlineError = deadline ? checkDeadline(deadline) : null;
  const available = channelsForKind(channels, plugins, kind);
  const info = resolveModelChannel(body, channels, plugins);
  const currentMissing = !!channelKey && !available.some((item) => item.key === channelKey);
  const setField = (field: string, value: unknown) =>
    onChange((current) => withModelField(current, field, value));
  const setNumber = (field: string, text: string) => {
    if (text.trim() === "") return setField(field, 0);
    const value = Number(text);
    if (Number.isFinite(value)) setField(field, value);
  };
  const insertTemplate = (id: "text" | "video") => {
    const template = MODEL_TEMPLATES.find((item) => item.id === id);
    if (!template) return;
    setField("input_schema", structuredClone(template.body.input_schema));
    setSchemaNonce((value) => value + 1);
  };

  return (
    <div className="flex flex-col gap-5 p-4">
      <section className="flex flex-col gap-3" aria-label="身份">
        <h3 className="text-sm font-medium">身份</h3>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <FormField
            label="key"
            htmlFor="model-key"
            error={issueFor(issues, "key")}
            hint={isNew ? "新建时填写，保存后不可修改" : "不可修改"}
            required
          >
            <div className="flex gap-1.5">
              <Input
                id="model-key"
                className="font-mono"
                value={readModelString(body, "key")}
                disabled={!isNew}
                aria-invalid={!!issueFor(issues, "key")}
                onChange={(event) => setField("key", event.target.value)}
              />
              {isNew && upstreamModel && !readModelString(body, "key") && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => setField("key", suggestModelKey(upstreamModel))}
                >
                  按上游模型建议
                </Button>
              )}
            </div>
          </FormField>
          <FormField label="kind" htmlFor="model-kind" error={issueFor(issues, "kind")} required>
            <NativeSelect
              id="model-kind"
              value={kind}
              aria-invalid={!!issueFor(issues, "kind")}
              onChange={(event) => setField("kind", event.target.value)}
            >
              {!MODEL_KINDS.includes(kind as (typeof MODEL_KINDS)[number]) && (
                <option value={kind}>{kind || "请选择"}</option>
              )}
              {MODEL_KINDS.map((item) => (
                <option key={item} value={item}>
                  {item}（{MODEL_KIND_LABEL[item]}）
                </option>
              ))}
            </NativeSelect>
          </FormField>
          <FormField
            label="展示名 label"
            htmlFor="model-label"
            error={issueFor(issues, "label")}
            required
          >
            <Input
              id="model-label"
              value={readModelString(body, "label")}
              aria-invalid={!!issueFor(issues, "label")}
              onChange={(event) => setField("label", event.target.value)}
            />
          </FormField>
          <FormField label="提示 hint" htmlFor="model-hint" hint="画布上模型名旁的一句说明，可留空">
            <Input
              id="model-hint"
              value={readModelString(body, "hint")}
              onChange={(event) => setField("hint", event.target.value)}
            />
          </FormField>
        </div>
      </section>

      <section className="flex flex-col gap-3" aria-label="渠道与上游模型">
        <h3 className="text-sm font-medium">渠道与上游模型</h3>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <FormField
            label="渠道"
            htmlFor="model-channel"
            error={issueFor(issues, "channels[0].channel") ?? issueFor(issues, "channels")}
            hint={`只列启用、且插件版本支持 ${kind || "该 kind"} 的渠道`}
            required
          >
            <NativeSelect
              id="model-channel"
              value={channelKey}
              aria-invalid={!!issueFor(issues, "channels[0].channel")}
              onChange={(event) =>
                onChange((current) => withModelChannel(current, event.target.value))
              }
            >
              <option value="">{channelsReady ? "请选择渠道" : "渠道加载中…"}</option>
              {currentMissing && (
                <option value={channelKey}>
                  {channelKey}（当前，{info.channel ? "不可用" : "找不到"}）
                </option>
              )}
              {available.map((item) => (
                <option key={item.key} value={item.key}>
                  {item.name}（{item.key}）
                </option>
              ))}
            </NativeSelect>
          </FormField>
          <FormField
            label="上游模型 upstream_model"
            htmlFor="model-upstream"
            error={issueFor(issues, "channels[0].upstream_model")}
            required
          >
            <Input
              id="model-upstream"
              className="font-mono"
              value={upstreamModel}
              aria-invalid={!!issueFor(issues, "channels[0].upstream_model")}
              onChange={(event) =>
                onChange((current) => withModelUpstream(current, event.target.value))
              }
            />
          </FormField>
        </div>

        {channelsReady && kind && available.length === 0 && !channelKey && (
          <Notice tone="warning">
            没有可用于 {kind} 的渠道。请联系运维在“渠道”页新建，或检查渠道与插件是否已停用。
          </Notice>
        )}

        {info.channel && (
          <div
            className="bg-muted flex flex-col gap-1.5 rounded-lg px-3 py-2 text-xs"
            data-testid="channel-info"
          >
            <div className="flex flex-wrap items-center gap-1.5">
              <span>
                插件 <b>{info.pluginName}</b>{" "}
                <span className="font-mono">v{info.pluginVersion}</span>
                {info.sha8 && (
                  <span className="text-muted-foreground font-mono"> · {info.sha8}</span>
                )}
              </span>
              {info.authLabel && (
                <span className="text-muted-foreground">· 鉴权：{info.authLabel}</span>
              )}
              {info.secretSet ? (
                <Tag tone="success">Key 已设置</Tag>
              ) : info.authType === "none" ? (
                <Tag>无需 Key</Tag>
              ) : (
                <Tag tone="warning">Key 未设置</Tag>
              )}
              {!info.channel.enabled && <Tag tone="danger">渠道已停用</Tag>}
            </div>
            {info.keyMissing && (
              <p className="text-amber-700 dark:text-amber-400">
                这个渠道还没有设置 Key，模型无法发布。请联系运维在{" "}
                <Link
                  className="underline"
                  to={`/admin/ai/channels?edit=${encodeURIComponent(info.channel.key)}`}
                >
                  渠道页
                </Link>{" "}
                设置。
              </p>
            )}
            {info.supportsKind === false && (
              <p className="text-destructive">这个渠道的插件版本不支持 {kind}，发布会被拒绝。</p>
            )}
          </div>
        )}
      </section>

      <section className="flex flex-col gap-3" aria-label="上架">
        <h3 className="text-sm font-medium">上架</h3>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <FormField
            label="积分 credits"
            htmlFor="model-credits"
            error={issueFor(issues, "credits")}
          >
            <Input
              id="model-credits"
              type="number"
              min={0}
              value={readModelNumber(body, "credits") ?? ""}
              aria-invalid={!!issueFor(issues, "credits")}
              onChange={(event) => setNumber("credits", event.target.value)}
            />
          </FormField>
          <FormField
            label="时限 deadline"
            htmlFor="model-deadline"
            error={deadlineError ?? issueFor(issues, "deadline")}
            hint="如 30m、1h"
          >
            <Input
              id="model-deadline"
              className="font-mono"
              value={deadline}
              aria-invalid={!!deadlineError}
              onChange={(event) => setField("deadline", event.target.value)}
            />
          </FormField>
          <FormField
            label="排序 sort"
            htmlFor="model-sort"
            error={issueFor(issues, "sort")}
            hint="越小越靠前"
          >
            <Input
              id="model-sort"
              type="number"
              value={readModelNumber(body, "sort") ?? ""}
              onChange={(event) => setNumber("sort", event.target.value)}
            />
          </FormField>
          <div className="flex min-w-0 flex-col gap-1.5">
            <Label htmlFor="model-enabled" className="text-muted-foreground text-xs">
              enabled（正文字段）
            </Label>
            <div className="flex h-9 items-center">
              <Switch
                id="model-enabled"
                checked={readModelBool(body, "enabled")}
                onCheckedChange={(checked) => setField("enabled", checked)}
              />
            </div>
            <p className="text-muted-foreground text-xs">发布后的实际上下架请用上方开关。</p>
          </div>
        </div>
      </section>

      <section className="flex flex-col gap-3" aria-label="固定参数">
        <JsonFieldEditor
          key={`params-${epoch}`}
          id="model-params"
          label="固定参数 params"
          hint="由插件定义，宿主只存不解释，原样交给插件。"
          value={body.params}
          issue={issueFor(issues, "params")}
          rows={5}
          onValid={(value) => setField("params", value)}
        />
      </section>

      <section className="flex flex-col gap-3" aria-label="输入定义">
        <JsonFieldEditor
          key={`schema-${epoch}-${schemaNonce}`}
          id="model-input-schema"
          label="输入定义 input_schema"
          hint="有序对象：书写顺序就是画布上的渲染顺序。"
          value={body.input_schema}
          issue={issueFor(issues, "input_schema")}
          rows={12}
          actions={
            <>
              <Button
                type="button"
                size="xs"
                variant="ghost"
                onClick={() => insertTemplate("text")}
              >
                插入文本模板
              </Button>
              <Button
                type="button"
                size="xs"
                variant="ghost"
                onClick={() => insertTemplate("video")}
              >
                插入视频模板
              </Button>
              <Button
                type="button"
                size="xs"
                variant="outline"
                onClick={() => setPreviewOpen(true)}
              >
                <Eye />
                预览画布表单
              </Button>
            </>
          }
          onValid={(value) => setField("input_schema", value)}
        />
      </section>

      <SchemaPreviewDialog
        open={previewOpen}
        schema={body.input_schema}
        onClose={() => setPreviewOpen(false)}
      />
    </div>
  );
}
