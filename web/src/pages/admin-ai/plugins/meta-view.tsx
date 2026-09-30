import type { ReactNode } from "react";

import type { PluginMeta } from "@/api/admin-ai/type";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { describeAuth, PLUGIN_KINDS } from "@/utils/admin/plugin";
import { initialSettingValues, settingFields } from "@/utils/admin/settings-form";

import { SettingFields } from "../setting-fields";
import { FoldableCode, Notice } from "../shared";

/** 一个区块：小标题 + 内容 */
function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h4 className="text-muted-foreground text-xs font-medium">{title}</h4>
      {children}
    </section>
  );
}

/** endpoint 模式的中文 */
const modeLabel = (mode: string | undefined) =>
  mode === "sync" ? "同步" : mode === "async" ? "异步" : (mode ?? "未知");

/** 设置项类型的中文 */
const TYPE_LABEL: Record<string, string> = {
  string: "文本",
  number: "数字",
  boolean: "开关",
  enum: "枚举",
};

/**
 * 插件能力清单：把 meta 摊成人能读的几块（鉴权、支持的生成方式、域名、渠道设置项、导入），
 * 不是 JSON 展示；原始 meta 折叠放在最后，供对照。
 */
export function MetaView({ meta }: { meta: PluginMeta | null | undefined }) {
  if (!meta) {
    return <p className="text-muted-foreground text-sm">这个版本没有读到 meta。</p>;
  }
  const authType = meta.auth?.type ?? "none";
  const endpoints = meta.endpoints && typeof meta.endpoints === "object" ? meta.endpoints : {};
  const hosts = Array.isArray(meta.allowedHosts) ? meta.allowedHosts : [];
  const settings = settingFields(meta.channelSettings);
  const importArgs = settingFields(meta.import?.args);
  const importable = !!meta.import;

  return (
    <div className="flex flex-col gap-5">
      {meta.description && <p className="text-muted-foreground text-sm">{meta.description}</p>}

      <Section title="鉴权">
        <p className="text-sm">{describeAuth(meta.auth)}</p>
        {authType === "custom" && (
          <Notice tone="warning">
            需要渠道开启 <code className="font-mono">allow_credentials</code>，才会把 Key
            交给这个插件；开启会记入审计日志。
          </Notice>
        )}
      </Section>

      <Section title="支持的生成方式">
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          {PLUGIN_KINDS.map((kind) => {
            const endpoint = (endpoints as Record<string, { mode?: string } | undefined>)[kind];
            const supported = !!endpoint;
            return (
              <div
                key={kind}
                className={cn(
                  "rounded-lg border p-2 text-center",
                  !supported && "bg-muted text-muted-foreground opacity-60",
                )}
                data-supported={supported}
              >
                <div className="text-sm font-medium">{kind}</div>
                <div className="text-muted-foreground text-xs">
                  {supported ? modeLabel(endpoint.mode) : "不支持"}
                </div>
              </div>
            );
          })}
        </div>
      </Section>

      <Section title="允许访问的域名">
        {hosts.length > 0 ? (
          <ul className="flex flex-wrap gap-1.5">
            {hosts.map((host) => (
              <li key={host} className="bg-muted rounded-md px-2 py-0.5 font-mono text-xs">
                {host}
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-muted-foreground text-sm">无（结果只能来自渠道 base_url）</p>
        )}
      </Section>

      <Section title="渠道设置项">
        {settings.length > 0 ? (
          <div className="rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>字段</TableHead>
                  <TableHead>标题</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead>必填</TableHead>
                  <TableHead>默认</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {settings.map((field) => (
                  <TableRow key={field.name}>
                    <TableCell className="font-mono text-xs">{field.name}</TableCell>
                    <TableCell>{field.label}</TableCell>
                    <TableCell>
                      {TYPE_LABEL[field.type] ?? field.type}
                      {field.options.length > 0 && (
                        <span className="text-muted-foreground">
                          {" "}
                          · {field.options.join(" / ")}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>{field.required ? "必填" : "可选"}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {field.default === undefined ? "" : String(field.default)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm">无</p>
        )}
      </Section>

      <Section title="导入模型">
        {!importable ? (
          <p className="text-muted-foreground text-sm">不支持导入模型</p>
        ) : importArgs.length === 0 ? (
          <p className="text-sm">支持（无参数，列出全部）</p>
        ) : (
          <div className="flex flex-col gap-2">
            <p className="text-sm">支持，需要下面的参数（预览，不可编辑）：</p>
            <SettingFields
              idPrefix="meta-import"
              fields={importArgs}
              values={initialSettingValues(importArgs, null)}
              disabled
              onChange={() => undefined}
            />
          </div>
        )}
      </Section>

      <Section title="原始 meta">
        <details className="group">
          <summary className="text-primary cursor-pointer text-xs hover:underline">
            展开只读 JSON
          </summary>
          <FoldableCode className="mt-2" text={JSON.stringify(meta, null, 2)} />
        </details>
      </Section>
    </div>
  );
}
