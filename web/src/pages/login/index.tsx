import { useEffect, useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { ArrowRight, Film, LoaderCircle } from "lucide-react";
import { toast } from "sonner";

import { getAuthConfig, login, type AuthConfig } from "@/api/auth";
import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DURATION } from "@/lib/motion";
import { mapAuthError } from "@/utils/auth/register-rules";
import { ApiError } from "@/utils/requests/request";
import { getToken, setToken } from "@/utils/storage/token";

import { PasswordField } from "./password-field";
import { RegisterForm } from "./register-form";

type Mode = "login" | "register";

function getDestination(search: string) {
  const next = new URLSearchParams(search).get("next");
  return next && next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/login")
    ? next
    : "/";
}

/**
 * 登录 / 注册同页：Tab 切换（淡入淡出，不位移），保留 ?next= 跳转。
 * 进页面先取 /auth/config：注册关闭时不显示注册入口；无需验证码时（首个账号）隐藏验证码。
 */
export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  const reduceMotion = useReducedMotion();
  const destination = getDestination(location.search);
  const wantsRegister = new URLSearchParams(location.search).get("tab") === "register";
  const [config, setConfig] = useState<AuthConfig | null>(null);
  const [mode, setMode] = useState<Mode>(wantsRegister ? "register" : "login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [loginError, setLoginError] = useState("");

  useEffect(() => {
    let alive = true;
    getAuthConfig()
      .then((value) => alive && setConfig(value))
      // 取不到配置时只保留登录；失败的全局 toast 已弹
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);

  if (getToken()) return <Navigate to={destination} replace />;

  const registerOpen = config?.register_enabled === true;
  const registerClosedHint = wantsRegister && config !== null && !registerOpen;
  const activeMode: Mode = mode === "register" && registerOpen ? "register" : "login";
  const fade = {
    initial: { opacity: 0 },
    animate: { opacity: 1, transition: { duration: reduceMotion ? 0 : DURATION.base } },
    exit: { opacity: 0, transition: { duration: reduceMotion ? 0 : DURATION.exit } },
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (loading) return;
    setLoading(true);
    setLoginError("");

    try {
      const result = await login({ username: username.trim(), password });
      setToken(result.token, result.expire_at, result.role);
      navigate(destination, { replace: true });
    } catch (error) {
      const hint = error instanceof ApiError ? mapAuthError(error.code) : null;
      if (hint?.field === "form") setLoginError(hint.message);
    } finally {
      setLoading(false);
    }
  };

  const handleRegistered = (firstUser: boolean) => {
    if (firstUser) toast.success("你是首个用户，已成为超级管理员，请到后台配置邮件服务");
    navigate(destination, { replace: true });
  };

  return (
    <main className="bg-background grid min-h-svh lg:grid-cols-2">
      <section className="relative hidden min-h-svh overflow-hidden bg-neutral-950 p-10 text-white lg:flex lg:flex-col lg:justify-between xl:p-14">
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_72%_35%,rgba(122,92,255,0.38),transparent_35%),radial-gradient(circle_at_22%_85%,rgba(45,128,176,0.22),transparent_38%)]" />
        <div className="absolute top-1/2 left-1/2 aspect-square w-[78%] -translate-x-1/2 -translate-y-1/2 rounded-full border border-white/10" />
        <div className="absolute top-1/2 left-1/2 aspect-square w-[56%] -translate-x-1/2 -translate-y-1/2 rounded-full border border-white/10" />
        <div className="relative flex items-center gap-3 text-lg font-semibold tracking-tight">
          <span className="flex size-10 items-center justify-center rounded-xl bg-white text-neutral-950">
            <Film className="size-5" aria-hidden="true" />
          </span>
          Video Canvas
        </div>
        <div className="relative max-w-xl">
          <span className="mb-6 inline-flex rounded-full border border-white/20 bg-white/5 px-3 py-1 text-xs text-white/75">
            让创意自由连接
          </span>
          <h1 className="text-5xl leading-tight font-semibold tracking-tight xl:text-6xl">
            从灵感到画面，
            <br />
            在一张画布上完成。
          </h1>
          <p className="mt-6 max-w-md text-base leading-7 text-white/65">
            组织想法、连接素材，继续你的创作。
          </p>
        </div>
        <p className="relative text-xs text-white/45">VIDEO CANVAS · CREATIVE WORKSPACE</p>
      </section>

      <section className="flex min-h-svh items-center justify-center px-6 py-12 sm:px-10">
        <div className="w-full max-w-[400px]">
          <div className="mb-12 flex items-center gap-3 text-base font-semibold lg:hidden">
            <span className="bg-foreground text-background flex size-9 items-center justify-center rounded-xl">
              <Film className="size-5" aria-hidden="true" />
            </span>
            Video Canvas
          </div>

          {registerOpen && (
            <Tabs
              value={activeMode}
              onValueChange={(value) => setMode(value as Mode)}
              className="mb-8"
            >
              <TabsList className="w-full">
                <TabsTrigger value="login">登录</TabsTrigger>
                <TabsTrigger value="register">注册</TabsTrigger>
              </TabsList>
            </Tabs>
          )}

          <AnimatePresence mode="wait" initial={false}>
            {activeMode === "login" ? (
              <motion.div key="login" {...fade}>
                <p className="text-muted-foreground mb-2 text-sm">欢迎回来</p>
                <h2 className="text-3xl font-semibold tracking-tight">登录你的账号</h2>
                <p className="text-muted-foreground mt-3">输入用户名和密码，继续使用画布。</p>

                {registerClosedHint && (
                  <Notice tone="warning" className="mt-6">
                    暂未开放注册
                  </Notice>
                )}

                <form className="mt-10 space-y-5" onSubmit={handleSubmit}>
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
                      onChange={(event) => setUsername(event.target.value)}
                      disabled={loading}
                      className="h-11"
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
                      onChange={(event) => setPassword(event.target.value)}
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
                    className="h-11 w-full justify-center"
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
              </motion.div>
            ) : (
              <motion.div key="register" {...fade}>
                <p className="text-muted-foreground mb-2 text-sm">加入 Video Canvas</p>
                <h2 className="text-3xl font-semibold tracking-tight">创建你的账号</h2>
                <p className="text-muted-foreground mt-3">
                  {config?.email_verify_required
                    ? "用邮箱验证码完成注册，注册后直接进入画布。"
                    : "填写用户名、邮箱和密码即可注册，注册后直接进入画布。"}
                </p>
                <div className="mt-8">
                  <RegisterForm
                    needCode={config?.email_verify_required ?? true}
                    onSuccess={handleRegistered}
                  />
                </div>
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      </section>
    </main>
  );
}
