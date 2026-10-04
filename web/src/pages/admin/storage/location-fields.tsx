import { ChevronRight } from "lucide-react";
import { useState } from "react";

import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { STORAGE_NAME_MAX } from "@/utils/admin/storage-form";
import { deriveEndpoint } from "@/utils/admin/storage-endpoint";

import { ADDRESSING_OPTIONS, PROVIDER_META } from "./provider-meta";
import { fieldLock, type SectionProps } from "./section-props";

/** 固定项的说明：预设强制 HTTPS 或固定寻址方式时，告诉管理员这些不用也不能自己选 */
function fixedNote(preset: SectionProps["preset"]) {
  if (!preset) return undefined;
  const parts: string[] = [];
  if (preset.force_ssl) parts.push("强制 HTTPS");
  if (preset.addressing === "virtual") parts.push("固定虚拟主机寻址");
  if (preset.addressing === "path") parts.push("固定路径寻址");
  return parts.length ? `${parts.join("，")}，无需设置。` : undefined;
}

/**
 * 基本信息：名称、地域（R2 换成 Account ID）、endpoint 只读推导、桶名、路径前缀。
 * Endpoint 只是按规则推导的展示，真正使用的值由后端按同样规则算出并保存。
 * 已被素材引用的定位字段禁用并带锁图标。
 */
export function LocationFields(props: SectionProps) {
  const { form, patch, errors, readOnly, preset } = props;
  const meta = PROVIDER_META[form.provider];
  const isR2 = form.provider === "r2";
  const region = fieldLock(props, "region");
  const account = fieldLock(props, "accountId");
  const bucket = fieldLock(props, "bucket");
  const prefix = fieldLock(props, "pathPrefix");
  const endpoint = fieldLock(props, "endpoint");
  const regions = preset?.regions ?? [];
  /** 已存的地域不在预设里时也要能显示，避免下拉空白 */
  const regionMissing = form.region !== "" && !regions.some((item) => item.id === form.region);
  const shownEndpoint =
    form.provider === "s3" && form.endpoint.trim()
      ? form.endpoint.trim()
      : deriveEndpoint(form.provider, { region: form.region, accountId: form.accountId });

  return (
    <div className="flex flex-col gap-3">
      <FormField label="名称" htmlFor="storage-name" error={errors.name} required>
        <Input
          id="storage-name"
          value={form.name}
          maxLength={STORAGE_NAME_MAX * 2}
          placeholder="例如：OSS 杭州 · 生产"
          disabled={readOnly}
          aria-invalid={!!errors.name}
          onChange={(event) => patch({ name: event.target.value })}
        />
      </FormField>

      {isR2 && (
        <FormField
          label={<>Account ID{account.lock}</>}
          htmlFor="storage-account"
          error={errors.accountId}
          hint="32 位十六进制，R2 概览页右侧可复制。Region 固定为 auto，无需选择"
          required
        >
          <Input
            id="storage-account"
            className="font-mono"
            value={form.accountId}
            placeholder="0123456789abcdef0123456789abcdef"
            disabled={account.disabled}
            aria-invalid={!!errors.accountId}
            onChange={(event) => patch({ accountId: event.target.value.trim() })}
          />
        </FormField>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        {isR2 ? (
          <FormField label="Region" htmlFor="storage-region">
            <Input id="storage-region" className="font-mono" value="auto" disabled />
          </FormField>
        ) : (
          <FormField
            label={<>地域{region.lock}</>}
            htmlFor="storage-region"
            error={errors.region}
            required
          >
            <NativeSelect
              id="storage-region"
              value={form.region}
              disabled={region.disabled || regions.length === 0}
              aria-invalid={!!errors.region}
              onChange={(event) => patch({ region: event.target.value })}
            >
              {form.region === "" && <option value="">请选择</option>}
              {regionMissing && <option value={form.region}>{form.region}</option>}
              {regions.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name} · {item.id}
                </option>
              ))}
            </NativeSelect>
          </FormField>
        )}
        <FormField label={<>Endpoint{endpoint.lock}</>} htmlFor="storage-endpoint">
          <Input
            id="storage-endpoint"
            className="font-mono text-xs"
            value={shownEndpoint}
            disabled
            readOnly
          />
        </FormField>
      </div>
      {fixedNote(preset) && (
        <p className="text-muted-foreground -mt-1 text-xs">{fixedNote(preset)}</p>
      )}

      <FormField
        label={<>Bucket{bucket.lock}</>}
        htmlFor="storage-bucket"
        error={errors.bucket}
        hint={meta.bucketHint}
        required
      >
        <Input
          id="storage-bucket"
          className="font-mono"
          value={form.bucket}
          placeholder={meta.bucketPlaceholder}
          disabled={bucket.disabled}
          aria-invalid={!!errors.bucket}
          onChange={(event) => patch({ bucket: event.target.value.trim() })}
        />
      </FormField>

      <FormField
        label={
          <>
            路径前缀<span className="text-muted-foreground font-normal">可选</span>
            {prefix.lock}
          </>
        }
        htmlFor="storage-prefix"
        error={errors.pathPrefix}
      >
        <Input
          id="storage-prefix"
          className="font-mono"
          value={form.pathPrefix}
          placeholder="assets/"
          disabled={prefix.disabled}
          aria-invalid={!!errors.pathPrefix}
          onChange={(event) => patch({ pathPrefix: event.target.value })}
        />
      </FormField>
    </div>
  );
}

/**
 * S3 的“高级”折叠区：自定义 endpoint（MinIO 等兼容服务）、寻址方式、HTTPS 开关。
 * 已经填过自定义配置时默认展开，免得管理员看不到自己改过的东西。
 */
export function AdvancedFields(props: SectionProps) {
  const { form, patch, errors, readOnly } = props;
  const [open, setOpen] = useState(
    () => form.endpoint.trim() !== "" || form.addressing !== "auto" || !form.useSSL,
  );
  const endpoint = fieldLock(props, "endpoint");
  const addressing = fieldLock(props, "addressing");

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex w-full items-center gap-1.5 py-1 text-sm font-medium">
        <ChevronRight className={cn("size-4 transition-transform", open && "rotate-90")} />
        高级
        <span className="text-xs font-normal">（自定义 Endpoint、寻址方式）</span>
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2 flex flex-col gap-3">
        <FormField
          label={<>自定义 Endpoint{endpoint.lock}</>}
          htmlFor="storage-custom-endpoint"
          error={errors.endpoint}
          hint="留空按地域推导。MinIO 等兼容服务填 host:port，不用写协议头"
        >
          <Input
            id="storage-custom-endpoint"
            className="font-mono"
            value={form.endpoint}
            placeholder="minio.internal:9000"
            disabled={endpoint.disabled}
            onChange={(event) => patch({ endpoint: event.target.value })}
          />
        </FormField>
        <FormField label={<>寻址方式{addressing.lock}</>} htmlFor="storage-addressing">
          <NativeSelect
            id="storage-addressing"
            value={form.addressing}
            disabled={addressing.disabled}
            onChange={(event) =>
              patch({ addressing: event.target.value as (typeof form)["addressing"] })
            }
          >
            {ADDRESSING_OPTIONS.map((item) => (
              <option key={item.value} value={item.value}>
                {item.label}
              </option>
            ))}
          </NativeSelect>
        </FormField>
        <div className="flex items-center gap-2 text-sm">
          <Switch
            id="storage-ssl"
            checked={form.useSSL}
            disabled={readOnly}
            onCheckedChange={(checked) => patch({ useSSL: checked })}
          />
          <Label htmlFor="storage-ssl">使用 HTTPS</Label>
          <span className="text-muted-foreground text-xs">内网 MinIO 没有证书时可以关闭</span>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
