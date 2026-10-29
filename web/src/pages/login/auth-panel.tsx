import type { FormEvent } from "react";
import { ArrowRight, LoaderCircle, LockKeyhole, UserRound } from "lucide-react";

import type { AuthConfig } from "@/api/auth";
import { Notice } from "@/components/admin-ui/notice";
import { Segmented } from "@/components/home/segmented";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

import { PasswordField } from "./password-field";
import { RegisterForm } from "./register-form";

/** 卡片里的两种表单 */
export type AuthMode = "login" | "register";

/** 卡片里的胶囊输入框：半透明底 + 细描边，左侧留出图标的位置 */
export const AUTH_INPUT =
  "bg-chrome-hover ring-chrome-border h-11 rounded-full border-0 pl-11 ring-1 focus-visible:ring-white/30 dark:bg-chrome-hover";

/** 胶囊输入框左侧的图标 */
export const AUTH_ICON =
  "text-muted-foreground pointer-events-none absolute top-1/2 left-4 size-4 -translate-y-1/2";

/** 卡片里的主按钮：白色实心胶囊 */
export const AUTH_SUBMIT = "mt-1 h-12 w-full justify-center rounded-full text-[15px]";

/**
 * 登录 / 注册卡片的内容（放在 IdeaDock 里）：注册开着时顶部是「登录 / 注册」分段，下面是对应表单。
 * 输入框是胶囊（标签只给读屏），焦点由 IdeaDock 在卡片展开后交给第一个输入框，表单里不 autoFocus。
 * 业务状态（输入值、请求、错误）都在页面里，这里只管版式和两种表单的切换。
 */
export function AuthPanel({
  mode,
  config,
  registerOpen,
  registerClosedHint,
  username,
  password,
  loading,
  loginError,
  onModeChange,
  onUsernameChange,
  onPasswordChange,
  onSubmit,
  onRegistered,
}: {
  mode: AuthMode;
  config: AuthConfig | null;
  /** 后台是否开放了注册：关闭时不显示注册入口 */
  registerOpen: boolean;
  /** 带着 ?tab=register 来但注册没开：登录表单上方提示 */
  registerClosedHint: boolean;
  username: string;
  password: string;
  /** 登录请求进行中 */
  loading: boolean;
  /** 登录失败的就地提示，空串表示没有 */
  loginError: string;
  onModeChange: (mode: AuthMode) => void;
  onUsernameChange: (value: string) => void;
  onPasswordChange: (value: string) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onRegistered: (firstUser: boolean) => void;
}) {
  return (
    <>
      {registerOpen && (
        <Segmented<AuthMode>
          variant="chrome"
          label="登录或注册"
          value={mode}
          onChange={onModeChange}
          className="mt-4 flex w-full *:flex-1"
          options={[
            { value: "login", label: "登录" },
            { value: "register", label: "注册" },
          ]}
        />
      )}

      {mode === "login" ? (
        <>
          {registerClosedHint && (
            <Notice tone="warning" className="mt-4">
              暂未开放注册
            </Notice>
          )}
          <form className="mt-4 space-y-3" onSubmit={onSubmit}>
            <div className="relative">
              <Label htmlFor="username" className="sr-only">
                用户名
              </Label>
              <UserRound className={AUTH_ICON} />
              <Input
                id="username"
                name="username"
                autoComplete="username"
                required
                minLength={3}
                maxLength={64}
                placeholder="用户名"
                value={username}
                onChange={(event) => onUsernameChange(event.target.value)}
                disabled={loading}
                className={AUTH_INPUT}
              />
            </div>
            <div className="relative">
              <Label htmlFor="password" className="sr-only">
                密码
              </Label>
              <LockKeyhole className={AUTH_ICON} />
              <PasswordField
                id="password"
                name="password"
                autoComplete="current-password"
                required
                minLength={6}
                maxLength={128}
                placeholder="密码"
                value={password}
                onChange={(event) => onPasswordChange(event.target.value)}
                disabled={loading}
                className={AUTH_INPUT}
              />
            </div>

            {loginError && (
              <p role="alert" className="text-destructive text-center text-sm">
                {loginError}
              </p>
            )}

            <Button type="submit" size="lg" disabled={loading} className={AUTH_SUBMIT}>
              {loading ? (
                <>
                  <LoaderCircle className="size-4 animate-spin" /> 登录中...
                </>
              ) : (
                <>
                  登录 <ArrowRight className="size-4" />
                </>
              )}
            </Button>
          </form>
        </>
      ) : (
        <div className="mt-4">
          <RegisterForm needCode={config?.email_verify_required ?? true} onSuccess={onRegistered} />
        </div>
      )}
    </>
  );
}
