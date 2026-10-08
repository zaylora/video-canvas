import { useEffect, type FormEvent } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { ArrowRight, LoaderCircle, X } from "lucide-react";

import type { AuthConfig } from "@/api/auth";
import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useIsMobile } from "@/hooks/use-mobile";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";

import { PasswordField } from "./password-field";
import { RegisterForm } from "./register-form";

/** 抽屉里的两种表单 */
export type AuthMode = "login" | "register";

/**
 * 登录 / 注册抽屉（设计稿 docs/品牌包装/登录与首页改版原型）：
 * 宽屏从右侧滑出，窄屏是底部上滑的面板；1180px 以下盖一层遮罩，更宽时背景和主文案还看得见（主文案会让出位置）。
 * Esc 或点遮罩关闭；打开时用户名输入框自动聚焦。
 * 业务状态（输入值、请求、错误）都在页面里，这里只管版式和两种表单的切换。
 */
export function AuthDrawer({
  open,
  mode,
  config,
  registerOpen,
  registerClosedHint,
  username,
  password,
  loading,
  loginError,
  onClose,
  onModeChange,
  onUsernameChange,
  onPasswordChange,
  onSubmit,
  onRegistered,
}: {
  open: boolean;
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
  onClose: () => void;
  onModeChange: (mode: AuthMode) => void;
  onUsernameChange: (value: string) => void;
  onPasswordChange: (value: string) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onRegistered: (firstUser: boolean) => void;
}) {
  const reduceMotion = useReducedMotion();
  const mobile = useIsMobile();

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open, onClose]);

  /** 宽屏从右滑入，窄屏从下滑入；减少动态效果时只淡入淡出 */
  const offset = reduceMotion ? {} : mobile ? { y: 24 } : { x: 24 };
  const fade = (duration: number) => ({ duration: reduceMotion ? 0 : duration, ease: EASE_OUT });

  return (
    <AnimatePresence>
      {open && (
        <>
          <motion.div
            key="backdrop"
            aria-hidden
            onClick={onClose}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1, transition: fade(DURATION.slow) }}
            exit={{ opacity: 0, transition: fade(DURATION.slowExit) }}
            className="fixed inset-0 z-20 bg-stage/40 min-[1180px]:hidden"
          />
          <motion.aside
            key="drawer"
            role="dialog"
            aria-modal="true"
            aria-labelledby="auth-title"
            initial={{ opacity: 0, ...offset }}
            animate={{ opacity: 1, x: 0, y: 0, transition: fade(DURATION.slow) }}
            exit={{ opacity: 0, ...offset, transition: fade(DURATION.slowExit) }}
            className="bg-background text-foreground border-border fixed z-30 flex flex-col overflow-auto border p-5 shadow-[0_30px_80px_-20px_oklch(0_0_0/0.6)] max-md:inset-x-0 max-md:bottom-0 max-md:max-h-[92svh] max-md:rounded-t-3xl md:top-4 md:right-4 md:bottom-4 md:w-[400px] md:rounded-3xl md:p-7"
          >
            <div className="flex items-center justify-between">
              <h2 id="auth-title" className="text-xl font-semibold tracking-tight">
                {mode === "login" ? "登录" : "加入连镜"}
              </h2>
              <motion.button
                type="button"
                whileTap={TAP}
                onClick={onClose}
                aria-label="关闭（Esc）"
                title="关闭（Esc）"
                className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 grid size-8 place-items-center rounded-lg outline-none focus-visible:ring-3"
              >
                <X className="size-4" />
              </motion.button>
            </div>
            <p className="text-muted-foreground mt-1.5 text-[13px]">
              {mode === "login"
                ? "登录后继续你的创作。"
                : config?.email_verify_required
                  ? "用邮箱验证码完成注册，注册后直接开始创作。"
                  : "填写用户名、邮箱和密码即可注册，注册后直接开始创作。"}
            </p>

            {registerOpen && (
              <Tabs
                value={mode}
                onValueChange={(value) => onModeChange(value as AuthMode)}
                className="mt-5"
              >
                <TabsList className="w-full">
                  <TabsTrigger value="login">登录</TabsTrigger>
                  <TabsTrigger value="register">注册</TabsTrigger>
                </TabsList>
              </Tabs>
            )}

            {mode === "login" ? (
              <>
                {registerClosedHint && (
                  <Notice tone="warning" className="mt-5">
                    暂未开放注册
                  </Notice>
                )}
                <form className="mt-5 space-y-4" onSubmit={onSubmit}>
                  <div className="space-y-2">
                    <Label htmlFor="username">用户名</Label>
                    <Input
                      id="username"
                      name="username"
                      autoComplete="username"
                      autoFocus
                      required
                      minLength={3}
                      maxLength={64}
                      placeholder="请输入用户名"
                      value={username}
                      onChange={(event) => onUsernameChange(event.target.value)}
                      disabled={loading}
                      className="h-10"
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="password">密码</Label>
                    <PasswordField
                      id="password"
                      name="password"
                      autoComplete="current-password"
                      required
                      minLength={6}
                      maxLength={128}
                      placeholder="请输入密码"
                      value={password}
                      onChange={(event) => onPasswordChange(event.target.value)}
                      disabled={loading}
                    />
                  </div>

                  {loginError && (
                    <p role="alert" className="text-destructive text-sm">
                      {loginError}
                    </p>
                  )}

                  <Button
                    type="submit"
                    size="lg"
                    disabled={loading}
                    className="h-10 w-full justify-center"
                  >
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
              <div className="mt-5">
                <RegisterForm
                  needCode={config?.email_verify_required ?? true}
                  onSuccess={onRegistered}
                />
              </div>
            )}
          </motion.aside>
        </>
      )}
    </AnimatePresence>
  );
}
