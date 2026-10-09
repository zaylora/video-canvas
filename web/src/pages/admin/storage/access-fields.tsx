import { Info } from "lucide-react";

import { ChoiceCard, ChoiceCardGroup } from "@/components/admin-ui/choice-card";
import {
  CopyBlock,
  CopyBlockCode,
  CopyBlockHeader,
  CopyBlockTitle,
} from "@/components/admin-ui/copy-block";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Tag } from "@/components/admin-ui/tag";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { corsRules } from "@/utils/admin/storage-endpoint";
import { ttlOptions } from "@/utils/admin/storage-rules";

import { DIRECT_METHOD_LABEL, PROVIDER_META } from "./provider-meta";
import type { SectionProps } from "./section-props";

/**
 * 访问方式：私有桶走签名地址（选有效期），公开读 / CDN 填公开域名。
 * 画布里存的是稳定地址 /files/<key>，签名有效期只影响单次访问，不会让画布里的素材失效。
 */
export function AccessFields({ form, patch, errors, readOnly }: SectionProps) {
  const meta = PROVIDER_META[form.provider];
  return (
    <div className="flex flex-col gap-3">
      <ChoiceCardGroup aria-label="访问方式" className="grid-cols-2">
        <ChoiceCard
          selected={form.access === "private"}
          disabled={readOnly}
          className="flex-col gap-0.5 p-2.5 data-selected:disabled:opacity-100"
          onClick={() => patch({ access: "private" })}
        >
          <span className="text-sm font-medium">私有桶 · 签名</span>
          <span className="text-muted-foreground text-xs">推荐。按需生成临时地址</span>
        </ChoiceCard>
        <ChoiceCard
          selected={form.access === "public"}
          disabled={readOnly}
          className="flex-col gap-0.5 p-2.5 data-selected:disabled:opacity-100"
          onClick={() => patch({ access: "public" })}
        >
          <span className="text-sm font-medium">公开读 / CDN</span>
          <span className="text-muted-foreground text-xs">地址固定，靠 key 不可猜测</span>
        </ChoiceCard>
      </ChoiceCardGroup>

      {form.access === "private" ? (
        <FormField
          label="签名有效期"
          htmlFor="storage-ttl"
          error={errors.signedTtlSec}
          hint={
            <>
              画布里存的是稳定地址 <code className="font-mono">/files/&lt;key&gt;</code>
              ，有效期只影响单次播放，不会让画布里的素材失效。
            </>
          }
        >
          <NativeSelect
            id="storage-ttl"
            value={form.signedTtlSec}
            disabled={readOnly}
            aria-invalid={!!errors.signedTtlSec}
            onChange={(event) => patch({ signedTtlSec: Number(event.target.value) })}
          >
            {ttlOptions(form.signedTtlSec).map((item) => (
              <option key={item.value} value={item.value}>
                {item.label}
              </option>
            ))}
          </NativeSelect>
        </FormField>
      ) : (
        <FormField
          label="公开访问域名"
          htmlFor="storage-public-url"
          error={errors.publicBaseUrl}
          hint={meta.publicHint || undefined}
          required
        >
          <Input
            id="storage-public-url"
            className="font-mono"
            value={form.publicBaseUrl}
            placeholder={
              form.provider === "r2"
                ? "https://pub-xxxx.r2.dev 或自定义域名"
                : "https://cdn.example.com"
            }
            disabled={readOnly}
            aria-invalid={!!errors.publicBaseUrl}
            onChange={(event) => patch({ publicBaseUrl: event.target.value })}
          />
        </FormField>
      )}
    </div>
  );
}

/**
 * 允许浏览器直传：开启后文件直接传到桶里，不经过后端，失败时自动回退到后端中转。
 * 直传方式跟着服务商预设走，管理员不用选；桶上需要配置 CORS，这里给出规则并一键复制。
 * MVP 不在这里真实探测浏览器直传（需要在浏览器里真正传一次），以桶上配好 CORS 为前提。
 */
export function DirectUploadFields({ form, patch, readOnly, preset }: SectionProps) {
  const method = preset?.direct_method;
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const rules = method ? corsRules(method, origin) : "";
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <Label htmlFor="storage-direct" className="text-sm font-semibold">
            允许浏览器直传
          </Label>
          <p className="text-muted-foreground mt-0.5 text-xs">
            开启后，用户上传的文件直接传到桶里，不经过后端；失败时会自动回退到后端中转。
          </p>
        </div>
        <Switch
          id="storage-direct"
          checked={form.directUpload}
          disabled={readOnly}
          onCheckedChange={(checked) => patch({ directUpload: checked })}
        />
      </div>
      {form.directUpload && method && (
        <>
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="text-muted-foreground">直传方式</span>
            <Tag tone={method === "presigned_put" ? "warning" : "neutral"}>
              {DIRECT_METHOD_LABEL[method]}
            </Tag>
          </div>
          <CopyBlock>
            <CopyBlockHeader>
              <CopyBlockTitle>
                <Info />
                需要在桶上配置 CORS
              </CopyBlockTitle>
              <CopyButton text={rules} label="复制规则" />
            </CopyBlockHeader>
            <CopyBlockCode>{rules}</CopyBlockCode>
            <p className="text-muted-foreground">
              请在云控制台的跨域设置里添加以上规则。此处不会自动验证浏览器直传，未配置时直传会失败并自动回退到后端中转。
            </p>
          </CopyBlock>
        </>
      )}
    </div>
  );
}
