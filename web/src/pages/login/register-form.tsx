import { useEffect, useState, type FormEvent } from "react";
import { ArrowRight, LoaderCircle } from "lucide-react";
import { toast } from "sonner";

import { register, sendRegisterCode } from "@/api/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/utils/requests/request";
import { setToken } from "@/utils/storage/token";
import {
  becameFirstAdmin,
  CODE_COOLDOWN_SECONDS,
  codeButtonState,
  hasRegisterErrors,
  mapAuthError,
  nextCountdown,
  validateEmail,
  validateRegisterForm,
  type RegisterErrors,
  type RegisterValues,
} from "@/utils/auth/register-rules";

import { PasswordField } from "./password-field";

const EMPTY: RegisterValues = { username: "", email: "", code: "", password: "", confirm: "" };

/** 字段 + 标签 + 就地错误 */
function Field({
  id,
  label,
  error,
  children,
}: {
  id: string;
  label: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error && (
        <p id={`${id}-error`} role="alert" className="text-destructive text-xs">
          {error}
        </p>
      )}
    </div>
  );
}

/**
 * 注册表单：用户名、邮箱、验证码（可隐藏）、密码、确认密码。
 * 字段校验走 utils/auth/register-rules；请求失败的全局 toast 由拦截器弹，这里只做字段级就地提示。
 * @param needCode 是否需要邮箱验证码；false 为全新环境的首个账号
 * @param onSuccess 注册成功（token 已写入）后的回调，带回是否成为了首个超级管理员（以返回的角色为准）
 */
export function RegisterForm({
  needCode,
  onSuccess,
}: {
  needCode: boolean;
  onSuccess: (firstUser: boolean) => void;
}) {
  const [values, setValues] = useState<RegisterValues>(EMPTY);
  const [errors, setErrors] = useState<RegisterErrors>({});
  const [formError, setFormError] = useState("");
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [remaining, setRemaining] = useState(0);

  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(() => setRemaining((value) => nextCountdown(value)), 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  const set = (key: keyof RegisterValues, value: string) => {
    setValues((prev) => ({ ...prev, [key]: value }));
    setErrors((prev) => (prev[key] ? { ...prev, [key]: undefined } : prev));
    setFormError("");
  };

  const applyHint = (code: unknown, message?: string) => {
    const hint = mapAuthError(code, message);
    if (!hint) return;
    if (hint.field === "form") setFormError(hint.message);
    else setErrors((prev) => ({ ...prev, [hint.field]: hint.message }));
  };

  const sendCode = async () => {
    const emailError = validateEmail(values.email);
    if (emailError) {
      setErrors((prev) => ({ ...prev, email: emailError }));
      return;
    }
    setSending(true);
    try {
      await sendRegisterCode({ email: values.email.trim() });
      setRemaining(CODE_COOLDOWN_SECONDS);
      toast.success("验证码已发送，10 分钟内有效");
    } catch (error) {
      if (error instanceof ApiError) applyHint(error.code);
    } finally {
      setSending(false);
    }
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (loading) return;
    const next = validateRegisterForm(values, needCode);
    setErrors(next);
    if (hasRegisterErrors(next)) return;
    setLoading(true);
    try {
      const result = await register({
        username: values.username.trim(),
        email: values.email.trim(),
        password: values.password,
        ...(needCode ? { code: values.code.trim() } : {}),
      });
      setToken(result.token, result.expire_at, result.role);
      onSuccess(becameFirstAdmin(result.role));
    } catch (error) {
      if (error instanceof ApiError) applyHint(error.code, error.message);
    } finally {
      setLoading(false);
    }
  };

  const codeButton = codeButtonState({
    remaining,
    sending,
    emailValid: validateEmail(values.email) === null,
  });

  return (
    <form className="space-y-4" onSubmit={handleSubmit} noValidate>
      <Field id="reg-username" label="用户名" error={errors.username}>
        <Input
          id="reg-username"
          autoComplete="username"
          autoFocus
          maxLength={64}
          placeholder="3 到 64 个字符"
          value={values.username}
          onChange={(event) => set("username", event.target.value)}
          disabled={loading}
          aria-invalid={!!errors.username}
          className="h-10"
        />
      </Field>
      <Field id="reg-email" label="邮箱" error={errors.email}>
        <Input
          id="reg-email"
          type="email"
          autoComplete="email"
          placeholder="name@example.com"
          value={values.email}
          onChange={(event) => set("email", event.target.value)}
          disabled={loading}
          aria-invalid={!!errors.email}
          className="h-10"
        />
      </Field>
      {needCode && (
        <Field id="reg-code" label="邮箱验证码" error={errors.code}>
          <div className="flex gap-2">
            <Input
              id="reg-code"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={6}
              placeholder="6 位数字"
              value={values.code}
              onChange={(event) => set("code", event.target.value.replace(/\D/g, ""))}
              disabled={loading}
              aria-invalid={!!errors.code}
              className="h-10"
            />
            <Button
              type="button"
              variant="outline"
              className="h-10 w-32 shrink-0 tabular-nums"
              disabled={codeButton.disabled || loading}
              onClick={() => void sendCode()}
            >
              {codeButton.label}
            </Button>
          </div>
        </Field>
      )}
      <Field id="reg-password" label="密码" error={errors.password}>
        <PasswordField
          id="reg-password"
          autoComplete="new-password"
          maxLength={128}
          placeholder="至少 8 位"
          value={values.password}
          onChange={(event) => set("password", event.target.value)}
          disabled={loading}
          aria-invalid={!!errors.password}
        />
      </Field>
      <Field id="reg-confirm" label="确认密码" error={errors.confirm}>
        <PasswordField
          id="reg-confirm"
          autoComplete="new-password"
          maxLength={128}
          placeholder="再输入一次密码"
          value={values.confirm}
          onChange={(event) => set("confirm", event.target.value)}
          disabled={loading}
          aria-invalid={!!errors.confirm}
        />
      </Field>

      {formError && (
        <p role="alert" className="text-destructive text-sm">
          {formError}
        </p>
      )}

      <Button type="submit" size="lg" disabled={loading} className="h-10 w-full justify-center">
        {loading ? (
          <>
            <LoaderCircle className="size-4 animate-spin" /> 注册中...
          </>
        ) : (
          <>
            注册并登录 <ArrowRight className="size-4" />
          </>
        )}
      </Button>
    </form>
  );
}
