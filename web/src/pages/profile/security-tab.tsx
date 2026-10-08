import { useEffect, useState, type FormEvent } from "react";
import { CircleAlert, LoaderCircle } from "lucide-react";
import { toast } from "sonner";

import { changePassword } from "@/api/me";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { PasswordField } from "@/pages/login/password-field";
import { useMeStore } from "@/store/me";
import {
  canSubmitPassword,
  formatLockCountdown,
  PASSWORD_LOCK_MS,
  passwordHints,
  submitPasswordChange,
  type FieldHint,
  type PasswordValues,
} from "@/utils/profile/password-form";
import { setToken } from "@/utils/storage/token";

const EMPTY: PasswordValues = { old: "", next: "", confirm: "" };

/**
 * 锁定截止时间（毫秒时间戳）。放在模块里而不是组件里：切到别的 tab 再回来倒计时还在；
 * 刷新页面会丢，但后端仍按 15 分钟锁定，再提交会重新拿到 55004。
 */
let lockedUntil = 0;

/** 提示颜色 */
const HINT_CLASS: Record<FieldHint["tone"], string> = {
  muted: "text-muted-foreground",
  ok: "text-status-success",
  error: "text-destructive",
};

/**
 * 一个密码字段：标签 + 带显示开关的输入框 + 下方提示（后端错误优先）
 */
function Field({
  id,
  label,
  autoComplete,
  placeholder,
  value,
  disabled,
  hint,
  onChange,
}: {
  id: string;
  label: string;
  autoComplete: string;
  placeholder: string;
  value: string;
  disabled: boolean;
  hint: FieldHint | null;
  onChange: (value: string) => void;
}) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <PasswordField
        id={id}
        autoComplete={autoComplete}
        placeholder={placeholder}
        maxLength={128}
        value={value}
        disabled={disabled}
        aria-invalid={hint?.tone === "error"}
        aria-describedby={`${id}-hint`}
        className="h-9"
        onChange={(event) => onChange(event.target.value)}
      />
      <p
        id={`${id}-hint`}
        aria-live="polite"
        className={cn("min-h-4 text-xs tabular-nums", hint && HINT_CLASS[hint.tone])}
      >
        {hint?.text}
      </p>
    </div>
  );
}

/**
 * 安全 tab：修改密码（设计 §6.7）。三个字段都带显示密码开关、允许粘贴、autocomplete 正确；
 * 前端即时提示只做参考，以后端为准。成功后用新令牌 setToken、toast、清空表单；
 * 55001 落在当前密码下（带剩余次数），55003/55002 落在新密码下，55004 在顶部提示并禁用按钮倒计时。
 */
export function SecurityTab() {
  const username = useMeStore((state) => state.me?.username ?? "");
  const [values, setValues] = useState<PasswordValues>(EMPTY);
  const [errors, setErrors] = useState<{ old?: string; next?: string }>({});
  const [banner, setBanner] = useState("");
  const [saving, setSaving] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const locked = lockedUntil > now;

  /** 锁定期间每秒刷新倒计时，到点自动解锁 */
  useEffect(() => {
    if (!locked) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [locked]);

  const hints = passwordHints(values, username);
  const canSubmit = canSubmitPassword(values, username, { locked, saving });

  const set = (key: keyof PasswordValues, value: string) => {
    setValues((prev) => ({ ...prev, [key]: value }));
    if (key !== "confirm") setErrors((prev) => ({ ...prev, [key]: undefined }));
    if (!locked) setBanner("");
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) return;
    setSaving(true);
    setErrors({});
    setBanner("");
    const result = await submitPasswordChange(values, { changePassword, setToken });
    setSaving(false);
    if (result.ok) {
      setValues(EMPTY);
      toast.success("密码已修改，其他设备已退出登录");
      return;
    }
    const hint = result.hint;
    if (!hint) return;
    if (hint.field === "form") {
      setBanner(hint.message);
      if (hint.lock) {
        lockedUntil = Date.now() + PASSWORD_LOCK_MS;
        setNow(Date.now());
      }
    } else {
      setErrors({ [hint.field]: hint.message });
    }
  };

  const errorHint = (text?: string): FieldHint | null => (text ? { tone: "error", text } : null);
  const shownBanner = banner || (locked ? "尝试过于频繁，请 15 分钟后再试" : "");

  return (
    <div className="grid items-start gap-3 md:grid-cols-2">
      <form
        className="bg-card ring-foreground/10 grid gap-3 rounded-xl p-4 ring-1"
        onSubmit={(event) => void submit(event)}
        noValidate
      >
        <div>
          <h2 className="text-sm font-semibold">修改密码</h2>
          <p className="text-muted-foreground text-xs">
            修改后，其他设备上的登录会立即失效，当前设备保持登录。
          </p>
        </div>
        {shownBanner && (
          <Alert variant="destructive">
            <CircleAlert />
            <AlertDescription>{shownBanner}</AlertDescription>
          </Alert>
        )}
        {/* 给密码管理器认出是哪个账号 */}
        <input
          type="text"
          name="username"
          autoComplete="username"
          value={username}
          readOnly
          hidden
        />
        <Field
          id="pw-old"
          label="当前密码"
          autoComplete="current-password"
          placeholder="输入当前密码"
          value={values.old}
          disabled={saving || locked}
          hint={errorHint(errors.old)}
          onChange={(value) => set("old", value)}
        />
        <Field
          id="pw-new"
          label="新密码"
          autoComplete="new-password"
          placeholder="至少 8 位"
          value={values.next}
          disabled={saving || locked}
          hint={errorHint(errors.next) ?? hints.next}
          onChange={(value) => set("next", value)}
        />
        <Field
          id="pw-confirm"
          label="确认新密码"
          autoComplete="new-password"
          placeholder="再输入一次"
          value={values.confirm}
          disabled={saving || locked}
          hint={hints.confirm}
          onChange={(value) => set("confirm", value)}
        />
        <Button type="submit" className="justify-self-start tabular-nums" disabled={!canSubmit}>
          {saving && <LoaderCircle className="animate-spin" />}
          {saving
            ? "提交中"
            : locked
              ? `请 ${formatLockCountdown(lockedUntil - now)} 后再试`
              : "修改密码"}
        </Button>
      </form>
      <section className="bg-card ring-foreground/10 grid gap-2 rounded-xl p-4 ring-1">
        <h2 className="text-sm font-semibold">规则说明</h2>
        <ul className="text-muted-foreground grid list-disc gap-1 pl-4 text-xs leading-relaxed">
          <li>新密码 8–72 字节，不要求大小写、数字、符号的组合。</li>
          <li>不能与当前密码相同，不能与用户名相同，也不能是常见弱密码。</li>
          <li>注册时用的是同一套规则。</li>
          <li>当前密码连续输错 5 次，锁定 15 分钟。</li>
        </ul>
      </section>
    </div>
  );
}
