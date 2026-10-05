import { useState } from "react";
import { KeyRound, Loader2, Send } from "lucide-react";
import { toast } from "sonner";

import { sendSmtpTest, updateSmtpSettings } from "@/api/admin/settings";
import type { SmtpSettings } from "@/api/admin/settings/type.d";
import { AdminMain } from "@/components/admin-ui/admin-main";
import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Notice } from "@/components/admin-ui/notice";
import {
  PageHeader,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { StatusLabel } from "@/components/admin-ui/status-dot";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";
import {
  SMTP_DEFAULT_PORT,
  canSaveSmtp,
  canSendTest,
  validateSmtpForm,
  type SmtpEncryption,
  type SmtpFormValues,
} from "@/utils/admin/smtp-rules";
import { ApiError } from "@/utils/requests/request";
import { formatTime } from "@/utils/time";

import { ReadOnlyNotice } from "../shared";
import { useAliveRef } from "../use-admin";
import { SettingsError, SettingsSkeleton } from "./settings-state";
import { SmtpPasswordDialog } from "./smtp-password-dialog";
import { useSmtpSettings } from "./use-settings";

const ENCRYPTION_LABEL: Record<SmtpEncryption, string> = {
  none: "无加密（none）",
  starttls: "STARTTLS",
  tls: "SSL / TLS",
};

const toValues = (settings: SmtpSettings): SmtpFormValues => ({
  host: settings.host,
  port: settings.port ? String(settings.port) : "",
  encryption: settings.encryption || "starttls",
  username: settings.username,
  from_address: settings.from_address,
  from_name: settings.from_name,
  enabled: settings.enabled,
});

/** 最近一次测试状态 */
function CheckStatus({ settings }: { settings: SmtpSettings }) {
  if (settings.last_check_ok === null || !settings.last_check_at) {
    return <StatusLabel tone="neutral">尚未测试</StatusLabel>;
  }
  return (
    <div className="flex flex-col gap-1">
      <StatusLabel tone={settings.last_check_ok ? "success" : "danger"}>
        {settings.last_check_ok ? "最近一次测试通过" : "最近一次测试失败"} ·{" "}
        {formatTime(settings.last_check_at)}
      </StatusLabel>
      {!settings.last_check_ok && settings.last_check_error && (
        <p className="text-muted-foreground text-xs">{settings.last_check_error}</p>
      )}
    </div>
  );
}

/** 发送测试邮件：用已保存的配置发，表单有未保存改动时先保存 */
function TestCard({
  settings,
  dirty,
  canWrite,
  onDone,
}: {
  settings: SmtpSettings;
  dirty: boolean;
  canWrite: boolean;
  onDone: () => void;
}) {
  const aliveRef = useAliveRef();
  const [to, setTo] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null);
  const configured = settings.host !== "" && settings.from_address !== "" && settings.has_password;
  const canSend = canSendTest({ dirty, configured, to, canWrite, busy });

  const send = async () => {
    if (!canSend) return;
    setBusy(true);
    setResult(null);
    try {
      await sendSmtpTest({ to: to.trim() });
      if (!aliveRef.current) return;
      setResult({ ok: true, message: `测试邮件已发送到 ${to.trim()}` });
    } catch (error) {
      // 全局 toast 已弹；这里把原因留在卡片里，方便对照修改配置
      if (aliveRef.current && error instanceof ApiError) {
        setResult({ ok: false, message: error.message });
      }
    } finally {
      if (aliveRef.current) {
        setBusy(false);
        onDone();
      }
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>发送测试邮件</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <CheckStatus settings={settings} />
        <div className="flex gap-2">
          <Input
            aria-label="测试收件邮箱"
            type="email"
            placeholder="收件邮箱"
            value={to}
            disabled={!canWrite || busy}
            onChange={(event) => setTo(event.target.value)}
          />
          <Button type="button" variant="outline" disabled={!canSend} onClick={() => void send()}>
            {busy ? <Loader2 className="animate-spin" /> : <Send />}
            发送
          </Button>
        </div>
        {canWrite && !configured && (
          <p className="text-muted-foreground text-xs">
            先保存服务器、发件人并设置密码，才能发测试邮件。
          </p>
        )}
        {canWrite && configured && dirty && (
          <p className="text-muted-foreground text-xs">有未保存的修改，先保存再测试。</p>
        )}
        {result && <Notice tone={result.ok ? "success" : "danger"}>{result.message}</Notice>}
      </CardContent>
    </Card>
  );
}

/** SMTP 表单与测试卡片 */
function SmtpForm({
  settings,
  canWrite,
  onSaved,
  reload,
}: {
  settings: SmtpSettings;
  canWrite: boolean;
  onSaved: (next: SmtpSettings) => void;
  reload: () => Promise<void>;
}) {
  const aliveRef = useAliveRef();
  const initial = toValues(settings);
  const [values, setValues] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const errors = validateSmtpForm(values);
  const canSave = canSaveSmtp({ values, initial, canWrite, saving });
  const dirty = JSON.stringify(values) !== JSON.stringify(initial);
  const patch = (next: Partial<SmtpFormValues>) => setValues((prev) => ({ ...prev, ...next }));
  const disabled = !canWrite || saving;

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      const next = await updateSmtpSettings({
        host: values.host.trim(),
        port: Number(values.port),
        encryption: values.encryption,
        username: values.username.trim(),
        from_address: values.from_address.trim(),
        from_name: values.from_name.trim(),
        enabled: values.enabled,
      });
      if (!aliveRef.current) return;
      toast.success("邮件服务已保存");
      setValues(toValues(next));
      onSaved(next);
    } catch {
      // 全局 toast 已弹（含内网地址等原因），表单保留当前输入
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
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
                <label htmlFor="smtp-enabled" className="text-sm font-medium">
                  启用邮件服务
                </label>
                <p className="text-muted-foreground text-xs">
                  启用后注册需要邮件验证码（首个账号免验证）；未启用时注册不验证邮箱，邮箱仍必填。
                </p>
              </div>
              <Switch
                id="smtp-enabled"
                checked={values.enabled}
                disabled={disabled}
                onCheckedChange={(checked) => patch({ enabled: checked })}
              />
            </div>
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_120px_160px]">
              <FormField label="服务器" htmlFor="smtp-host" required error={errors.host}>
                <Input
                  id="smtp-host"
                  placeholder="smtp.example.com"
                  autoComplete="off"
                  value={values.host}
                  disabled={disabled}
                  aria-invalid={!!errors.host}
                  onChange={(event) => patch({ host: event.target.value })}
                />
              </FormField>
              <FormField label="端口" htmlFor="smtp-port" required error={errors.port}>
                <Input
                  id="smtp-port"
                  inputMode="numeric"
                  value={values.port}
                  disabled={disabled}
                  aria-invalid={!!errors.port}
                  onChange={(event) => patch({ port: event.target.value })}
                />
              </FormField>
              <FormField label="加密" htmlFor="smtp-encryption">
                <NativeSelect
                  id="smtp-encryption"
                  value={values.encryption}
                  disabled={disabled}
                  onChange={(event) => {
                    const encryption = event.target.value as SmtpEncryption;
                    // 端口还是上一种加密方式的默认值时，顺手换成新方式的默认值
                    const isDefault = values.port === SMTP_DEFAULT_PORT[values.encryption];
                    patch({
                      encryption,
                      ...(isDefault ? { port: SMTP_DEFAULT_PORT[encryption] } : {}),
                    });
                  }}
                >
                  {(Object.keys(ENCRYPTION_LABEL) as SmtpEncryption[]).map((key) => (
                    <option key={key} value={key}>
                      {ENCRYPTION_LABEL[key]}
                    </option>
                  ))}
                </NativeSelect>
              </FormField>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField label="用户名" htmlFor="smtp-username" hint="免认证的 SMTP 可留空。">
                <Input
                  id="smtp-username"
                  autoComplete="off"
                  value={values.username}
                  disabled={disabled}
                  onChange={(event) => patch({ username: event.target.value })}
                />
              </FormField>
              <FormField
                label="密码"
                htmlFor="smtp-password-state"
                hint="密码只写不读，保存后不会再显示。"
              >
                <div className="flex items-center gap-2">
                  <Input
                    id="smtp-password-state"
                    readOnly
                    value={settings.has_password ? "已设置" : "未设置"}
                    className="text-muted-foreground"
                  />
                  {canWrite && (
                    <Button type="button" variant="outline" onClick={() => setPasswordOpen(true)}>
                      <KeyRound />
                      {settings.has_password ? "替换密码" : "设置密码"}
                    </Button>
                  )}
                </div>
              </FormField>
              <FormField
                label="发件人地址"
                htmlFor="smtp-from-address"
                required
                error={errors.from_address}
              >
                <Input
                  id="smtp-from-address"
                  type="email"
                  placeholder="noreply@example.com"
                  value={values.from_address}
                  disabled={disabled}
                  aria-invalid={!!errors.from_address}
                  onChange={(event) => patch({ from_address: event.target.value })}
                />
              </FormField>
              <FormField label="发件人名称" htmlFor="smtp-from-name">
                <Input
                  id="smtp-from-name"
                  placeholder="Video Canvas"
                  value={values.from_name}
                  disabled={disabled}
                  onChange={(event) => patch({ from_name: event.target.value })}
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

      <TestCard
        settings={settings}
        dirty={dirty}
        canWrite={canWrite}
        onDone={() => void reload()}
      />

      <SmtpPasswordDialog
        open={passwordOpen}
        onClose={() => setPasswordOpen(false)}
        onReplaced={() => void reload()}
      />
    </div>
  );
}

/**
 * 邮件服务页（/admin/settings/email）：SMTP 配置、密码替换、发送测试邮件；super_admin 可改，admin 只读。
 * 请求错误的全局提示由拦截器弹，这里不重复。
 */
export default function EmailSettingsPage() {
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const { data, status, reload, setData } = useSmtpSettings();

  return (
    <div className="h-full overflow-y-auto">
      <AdminMain>
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>邮件服务</PageHeaderTitle>
            <PageHeaderDescription>
              用于发送注册验证码。SMTP 密码加密存储，保存后不会再显示。
            </PageHeaderDescription>
          </PageHeaderHeading>
        </PageHeader>

        {!canWrite && (
          <ReadOnlyNotice className="mb-4" what="修改邮件服务、替换密码、发送测试邮件" />
        )}

        {status === "loading" && <SettingsSkeleton />}
        {status === "error" && <SettingsError onRetry={() => void reload()} />}
        {status === "ready" && data && (
          <SmtpForm settings={data} canWrite={canWrite} onSaved={setData} reload={reload} />
        )}
      </AdminMain>
    </div>
  );
}
