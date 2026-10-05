import type { ReactNode } from "react";

import type { StoragePreset } from "@/api/admin/storage/type.d";
import { LockHint } from "@/components/admin-ui/lock-hint";
import {
  type StorageFormErrors,
  type StorageFormField,
  type StorageFormState,
} from "@/utils/admin/storage-form";
import { isFieldLocked, lockReason } from "@/utils/admin/storage-rules";

/** 抽屉里各分区共用的属性 */
export type SectionProps = {
  /** 表单状态 */
  form: StorageFormState;
  /** 局部更新表单 */
  patch: (partial: Partial<StorageFormState>) => void;
  /** 要显示的校验错误（已按“提交过或已输入”过滤） */
  errors: StorageFormErrors;
  /** 只读：admin 查看，所有输入禁用 */
  readOnly: boolean;
  /** 是否在编辑已有存储 */
  editing: boolean;
  /** 已有素材引用：定位字段锁定 */
  locked: boolean;
  /** 引用这套存储的素材数，写进锁定说明 */
  assetCount: number;
  /** 当前服务商的预设；预设还没加载时为空 */
  preset: StoragePreset | undefined;
};

/**
 * 某个字段的禁用状态与锁图标：只读或被素材引用锁定时禁用；锁定时返回带原因的锁图标放在标题旁
 * @param props 分区属性
 * @param field 表单字段
 */
export function fieldLock(
  props: SectionProps,
  field: StorageFormField,
): { disabled: boolean; lock: ReactNode } {
  const locked = isFieldLocked(field, { editing: props.editing, locked: props.locked });
  return {
    disabled: props.readOnly || locked,
    lock: locked ? <LockHint reason={lockReason(props.assetCount)} /> : null,
  };
}
