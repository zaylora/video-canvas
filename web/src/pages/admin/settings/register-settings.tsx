import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { updateRegisterSettings } from "@/api/admin/settings";
import type { RegisterSettings } from "@/api/admin/settings/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";
import {
  canSaveRegisterSettings,
  validateRegisterSettings,
  type RegisterSettingsValues,
} from "@/utils/admin/smtp-rules";

import { ReadOnlyNotice } from "../shared";
import { useAliveRef } from "../use-admin";
import { SettingsError, SettingsSkeleton } from "./settings-state";
import { useRegisterSettings } from "./use-settings";

const toValues = (settings: RegisterSettings): RegisterSettingsValues => ({
  register_enabled: settings.register_enabled,
  initial_credits: String(settings.initial_credits),
  default_max_active_tasks: String(settings.default_max_active_tasks),
});

/**
 * 注册设置表单：开放注册开关、新用户初始积分、默认并发上限。
 * @param settings 已保存的设置
 * @param canWrite 是否可写（super_admin）
 * @param onSaved 保存成功后带回服务端返回的设置
 */
function RegisterForm({
  settings,
  canWrite,
  onSaved,
}: {
  settings: RegisterSettings;
  canWrite: boolean;
  onSaved: (next: RegisterSettings) => void;
}) {
  const aliveRef = useAliveRef();
  const initial = toValues(settings);
  const [values, setValues] = useState(initial);
  const [saving, setSaving] = useState(false);
  const errors = validateRegisterSettings(values);
  const canSave = canSaveRegisterSettings({ values, initial, canWrite, saving });

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      const next = await updateRegisterSettings({
        register_enabled: values.register_enabled,
        initial_credits: Number(values.initial_credits),
        default_max_active_tasks: Number(values.default_max_active_tasks),
      });
      if (!aliveRef.current) return;
      toast.success("注册设置已保存");
      setValues(toValues(next));
      onSaved(next);
    } catch {
      // 全局 toast 已弹，表单保留当前输入
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <Card>
        <CardContent className="flex flex-col gap-5">
          <div className="flex items-center justify-between gap-4">
            <div>
              <label htmlFor="register-enabled" className="text-sm font-medium">
                开放注册
              </label>
              <p className="text-muted-foreground text-xs">
                关闭后登录页不再显示注册入口，注册接口也会拒绝请求。
              </p>
            </div>
            <Switch
              id="register-enabled"
              checked={values.register_enabled}
              disabled={!canWrite || saving}
              onCheckedChange={(checked) =>
                setValues((prev) => ({ ...prev, register_enabled: checked }))
              }
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              label="新用户初始积分"
              htmlFor="register-credits"
              hint="注册成功时发放，并记一条流水。"
              error={errors.initial_credits}
            >
              <Input
                id="register-credits"
                inputMode="numeric"
                value={values.initial_credits}
                disabled={!canWrite || saving}
                aria-invalid={!!errors.initial_credits}
                onChange={(event) =>
                  setValues((prev) => ({ ...prev, initial_credits: event.target.value }))
                }
              />
            </FormField>
            <FormField
              label="默认并发上限"
              htmlFor="register-limit"
              hint="每个用户同时进行的任务数（1 到 64），可在用户详情里单独覆盖。"
              error={errors.default_max_active_tasks}
            >
              <Input
                id="register-limit"
                inputMode="numeric"
                value={values.default_max_active_tasks}
                disabled={!canWrite || saving}
                aria-invalid={!!errors.default_max_active_tasks}
                onChange={(event) =>
                  setValues((prev) => ({ ...prev, default_max_active_tasks: event.target.value }))
                }
              />
            </FormField>
          </div>
        </CardContent>
        {canWrite && (
          <CardFooter className="justify-end">
            <Button type="submit" disabled={!canSave}>
              {saving && <Loader2 className="animate-spin" />}
              保存
            </Button>
          </CardFooter>
        )}
      </Card>
    </form>
  );
}

/**
 * 注册设置页（/admin/settings/register）：super_admin 可改，admin 只读。
 * 请求错误的全局提示由拦截器弹，这里不重复。
 */
export default function RegisterSettingsPage() {
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const { data, status, reload, setData } = useRegisterSettings();

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>注册设置</PageHeaderTitle>
          </PageHeaderHeading>
        </PageHeader>

        {!canWrite && <ReadOnlyNotice className="mb-4" what="修改注册设置" />}

        {status === "loading" && <SettingsSkeleton />}
        {status === "error" && <SettingsError onRetry={() => void reload()} />}
        {status === "ready" && data && (
          <RegisterForm settings={data} canWrite={canWrite} onSaved={setData} />
        )}
      </main>
    </div>
  );
}
