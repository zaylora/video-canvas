import { KeyRound } from "lucide-react";

import { FormField } from "@/components/admin-ui/form-field";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

import { PROVIDER_META } from "./provider-meta";
import type { SectionProps } from "./section-props";

/**
 * 访问凭证。新建时填 AccessKey ID 与 Secret（只写不读，type=password）；
 * 编辑时显示脱敏的 AccessKey ID 和“已设置”，要换只能点“替换”走单独的接口：
 * 两者必须一起换，后端先用新凭证测试，不通过就什么都不改。
 * @param secretSet 编辑时 Secret 是否已设置
 * @param onReplace 点“替换”（只对 super_admin 显示）
 */
export function CredentialFields({
  secretSet,
  onReplace,
  ...props
}: SectionProps & { secretSet: boolean; onReplace?: () => void }) {
  const { form, patch, errors, readOnly, editing } = props;
  const meta = PROVIDER_META[form.provider];

  if (editing) {
    return (
      <div className="flex flex-col gap-2">
        <FormField label={meta.keyLabel} htmlFor="storage-key-masked">
          <div className="flex items-center gap-2">
            <Input
              id="storage-key-masked"
              className="font-mono"
              value={form.accessKeyId}
              disabled
              readOnly
            />
            {secretSet ? (
              <Tag tone="success">Secret 已设置</Tag>
            ) : (
              <Tag tone="warning">Secret 未设置</Tag>
            )}
            {!readOnly && onReplace && (
              <Button type="button" variant="outline" onClick={onReplace}>
                <KeyRound />
                替换
              </Button>
            )}
          </div>
        </FormField>
        <p className="text-muted-foreground text-xs">
          凭证只写不读，保存后无法查看。{meta.credentialHint}
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <FormField label={meta.keyLabel} htmlFor="storage-key" error={errors.accessKeyId} required>
        <Input
          id="storage-key"
          className="font-mono"
          autoComplete="off"
          spellCheck={false}
          value={form.accessKeyId}
          disabled={readOnly}
          aria-invalid={!!errors.accessKeyId}
          onChange={(event) => patch({ accessKeyId: event.target.value })}
        />
      </FormField>
      <FormField
        label={meta.secretLabel}
        htmlFor="storage-secret"
        error={errors.secretKey}
        hint="只写不读：保存后不会再显示，也不会出现在任何请求地址或缓存里"
        required
      >
        <Input
          id="storage-secret"
          type="password"
          className="font-mono"
          autoComplete="new-password"
          spellCheck={false}
          value={form.secretKey}
          disabled={readOnly}
          aria-invalid={!!errors.secretKey}
          onChange={(event) => patch({ secretKey: event.target.value })}
        />
      </FormField>
      <p className="text-muted-foreground text-xs">{meta.credentialHint}</p>
    </div>
  );
}
