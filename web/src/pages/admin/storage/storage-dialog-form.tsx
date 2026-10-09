import { Loader2, Stethoscope } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import {
  checkStorage,
  createStorage,
  getStorage,
  testStorageDraft,
  updateStorage,
} from "@/api/admin/storage";
import type { StoragePreset, StorageView } from "@/api/admin/storage/type.d";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";
import { DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { isStorageFieldLocked, isStorageVersionConflict } from "@/utils/admin/errors";
import {
  buildCreateBody,
  buildTestBody,
  buildUpdateBody,
  changeProvider,
  emptyStorageForm,
  storageFormFromView,
  validateStorageForm,
  type StorageFormErrors,
  type StorageFormField,
  type StorageFormState,
} from "@/utils/admin/storage-form";
import { formatCount } from "@/utils/admin/storage-rules";

import { useAliveRef } from "../use-admin";
import { ReadOnlyNotice } from "../shared";
import { AccessFields, DirectUploadFields } from "./access-fields";
import { CredentialFields } from "./credential-fields";
import { AdvancedFields, LocationFields } from "./location-fields";
import { ProbePanel } from "./probe-panel";
import { ProviderPicker } from "./provider-picker";
import { SecretDialog } from "./secret-dialog";
import type { SectionProps } from "./section-props";
import { useStorageProbe } from "./use-storage-probe";

/** 改了这些字段会让“用草稿测试”的结果作废 */
const CONNECTION_FIELDS: StorageFormField[] = [
  "provider",
  "region",
  "accountId",
  "endpoint",
  "bucket",
  "pathPrefix",
  "addressing",
  "useSSL",
  "accessKeyId",
  "secretKey",
  "access",
  "publicBaseUrl",
];

/** 测试连接只关心这些字段的校验结果 */
const TEST_FIELDS: StorageFormField[] = [
  "region",
  "accountId",
  "endpoint",
  "bucket",
  "pathPrefix",
  "accessKeyId",
  "secretKey",
  "publicBaseUrl",
];

/** 输入过就即时提示规则错误的字段；其余字段等提交过再显示，免得没填完就一片红 */
const LIVE_FIELDS: StorageFormField[] = ["bucket", "accountId", "pathPrefix", "publicBaseUrl"];

/**
 * 弹窗里的表单：新建或编辑一套存储。表单状态只在这里；父级用 key 控制“何时重新初始化”。
 * @param storage 编辑的存储；新建时为 null
 * @param presets 服务商预设
 * @param canWrite 是否有写权限；没有则整个表单只读
 * @param onSaved 创建 / 更新成功后
 * @param onChanged 这条存储在弹窗里变了（测试结果、替换凭证、冲突后重新拉取）
 * @param onConflict 版本冲突或定位字段被锁、已重新拉取之后：让父级重置表单
 * @param onDirtyChange 上报是否有未保存的修改，关闭时据此确认
 * @param onClose 请求关闭
 */
export function StorageForm({
  storage,
  presets,
  canWrite,
  onSaved,
  onChanged,
  onConflict,
  onDirtyChange,
  onClose,
}: {
  storage: StorageView | null;
  presets: StoragePreset[];
  canWrite: boolean;
  onSaved: () => void;
  onChanged: (view: StorageView) => void;
  onConflict: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onClose: () => void;
}) {
  const aliveRef = useAliveRef();
  const editing = !!storage;
  const readOnly = !canWrite;
  const [form, setForm] = useState<StorageFormState>(() =>
    storage ? storageFormFromView(storage) : emptyStorageForm(presets),
  );
  const [baseline] = useState(() => JSON.stringify(form));
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [secretOpen, setSecretOpen] = useState(false);
  const probe = useStorageProbe();

  /** 弹窗卸载（关闭、切换对象）时清掉脏标记，避免下次打开误提示“放弃修改” */
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const update = (next: StorageFormState) => {
    setForm(next);
    onDirtyChange(JSON.stringify(next) !== baseline);
  };
  const patch = (partial: Partial<StorageFormState>) => {
    if (
      probe.state.kind === "done" &&
      probe.state.source === "draft" &&
      CONNECTION_FIELDS.some((field) => field in partial)
    ) {
      probe.reset();
    }
    update({ ...form, ...partial });
  };

  const locked = storage?.locked ?? false;
  const errors = validateStorageForm(form, { editing, locked });
  const visibleErrors: StorageFormErrors = Object.fromEntries(
    Object.entries(errors).filter(
      ([field]) =>
        submitted ||
        (LIVE_FIELDS.includes(field as StorageFormField) &&
          form[field as keyof StorageFormState] !== ""),
    ),
  );
  const preset = presets.find((item) => item.provider === form.provider);
  const section: SectionProps = {
    form,
    patch,
    errors: visibleErrors,
    readOnly,
    editing,
    locked,
    assetCount: storage?.asset_count ?? 0,
    preset,
  };

  /** 保存、重新测试之后拉一次最新视图（测试结果、版本号都在后端变了） */
  const refetch = async () => {
    if (!storage) return null;
    try {
      const fresh = await getStorage(storage.id);
      if (aliveRef.current) onChanged(fresh);
      return fresh;
    } catch {
      // 全局 toast 已弹；列表保持旧数据
      return null;
    }
  };

  /** 测试连接：编辑已保存的存储用 /check（密钥在后端），新建用草稿测试（不落库） */
  const runTest = async () => {
    if (storage) {
      const result = await probe.run("saved", () => checkStorage(storage.id));
      if (result) await refetch();
      return;
    }
    if (TEST_FIELDS.some((field) => errors[field])) {
      setSubmitted(true);
      return;
    }
    await probe.run("draft", () => testStorageDraft(buildTestBody(form)));
  };

  /** 版本冲突或定位字段被锁：别人改过 / 素材刚引用了这套存储，重新拉取并重置表单 */
  const resync = async (conflict: boolean) => {
    const fresh = await refetch();
    if (!fresh || !aliveRef.current) return;
    if (conflict) toast.warning("该存储刚被其他人修改，已刷新为最新内容，请确认后重新保存");
    onConflict();
  };

  const save = async () => {
    setSubmitted(true);
    if (Object.keys(errors).length > 0 || saving) return;
    setSaving(true);
    try {
      const view = storage
        ? await updateStorage(storage.id, buildUpdateBody(form, storage.version))
        : await createStorage(buildCreateBody(form));
      if (view.check && !view.check.ok) {
        toast.warning("已保存，但测试未通过：修复前不能设为默认");
      } else {
        toast.success(storage ? "已保存" : "已创建。测试通过后可以设为默认");
      }
      onDirtyChange(false);
      onSaved();
    } catch (error) {
      if (storage && isStorageVersionConflict(error)) await resync(true);
      else if (storage && isStorageFieldLocked(error)) await resync(false);
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  const testing = probe.state.kind === "running";
  const draftFailed =
    probe.state.kind === "done" && probe.state.source === "draft" && !probe.state.result.ok;
  const errorCount = Object.keys(errors).length;
  const title = readOnly ? "查看存储" : editing ? "编辑存储" : "新建存储";

  return (
    <>
      <DialogHeader className="gap-0.5 border-b p-4 pr-12">
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>
          {editing && locked && storage
            ? `已有 ${formatCount(storage.asset_count)} 个素材，定位字段已锁定`
            : "保存时会自动测试连接"}
        </DialogDescription>
      </DialogHeader>

      <form
        id="storage-form"
        className="flex min-h-0 flex-1 flex-col overflow-y-auto p-4"
        onSubmit={(event) => {
          event.preventDefault();
          if (!readOnly) void save();
        }}
      >
        {readOnly && <ReadOnlyNotice what="修改存储配置" className="mb-5" />}
        {submitted && errorCount > 0 && (
          <Notice tone="danger" className="mb-5">
            还有 {errorCount} 项需要修正，请看标红的字段。
          </Notice>
        )}

        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>服务商</FormSectionTitle>
            {editing && <FormSectionDescription>创建后不可更改。</FormSectionDescription>}
          </FormSectionHeader>
          <ProviderPicker
            presets={presets}
            value={form.provider}
            disabled={readOnly || editing}
            onChange={(provider) => {
              probe.reset();
              update(changeProvider(form, provider, presets));
            }}
          />
        </FormSection>

        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>基本信息</FormSectionTitle>
          </FormSectionHeader>
          <LocationFields {...section} />
          {form.provider === "s3" && (
            <div className="mt-3">
              <AdvancedFields {...section} />
            </div>
          )}
        </FormSection>

        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>访问凭证</FormSectionTitle>
          </FormSectionHeader>
          <CredentialFields
            {...section}
            form={storage ? { ...form, accessKeyId: storage.access_key_id } : form}
            secretSet={storage?.secret_set ?? false}
            onReplace={storage ? () => setSecretOpen(true) : undefined}
          />
        </FormSection>

        <FormSection>
          <FormSectionHeader>
            <FormSectionTitle>访问方式</FormSectionTitle>
          </FormSectionHeader>
          <AccessFields {...section} />
        </FormSection>

        <FormSection>
          <DirectUploadFields {...section} />
        </FormSection>

        <FormSection>
          <ProbePanel state={probe.state} withDirect={form.directUpload} />
          {editing && !readOnly && (
            <p className="text-muted-foreground mt-2 text-xs">
              “测试连接”使用已保存的配置与密钥；修改连接字段并保存后会自动重新测试。
            </p>
          )}
        </FormSection>
      </form>

      <DialogFooter className="m-0 flex-row items-center rounded-none border-t bg-transparent p-4 sm:justify-start">
        {!readOnly && (
          <Button
            type="button"
            variant="outline"
            disabled={testing || saving}
            onClick={() => void runTest()}
          >
            {testing ? <Loader2 className="animate-spin" /> : <Stethoscope />}
            测试连接
          </Button>
        )}
        <div className="ml-auto flex items-center gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            {readOnly ? "关闭" : "取消"}
          </Button>
          {!readOnly && (
            <Button type="submit" form="storage-form" disabled={testing || saving}>
              {saving && <Loader2 className="animate-spin" />}
              {draftFailed ? "仍然保存（不可设为默认）" : "保存"}
            </Button>
          )}
        </div>
      </DialogFooter>

      {storage && (
        <SecretDialog
          open={secretOpen}
          storage={storage}
          onClose={() => setSecretOpen(false)}
          onReplaced={(view) => {
            probe.reset();
            onChanged(view);
          }}
        />
      )}
    </>
  );
}
