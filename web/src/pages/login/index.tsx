import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { ArrowRight, Eye, EyeOff, Film, LoaderCircle } from "lucide-react";

import { login } from "@/api/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { getToken, setToken } from "@/utils/storage/token";

function getDestination(search: string) {
  const next = new URLSearchParams(search).get("next");
  return next && next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/login")
    ? next
    : "/";
}

export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  const destination = getDestination(location.search);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);

  if (getToken()) return <Navigate to={destination} replace />;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (loading) return;
    setLoading(true);

    try {
      const result = await login({ username: username.trim(), password });
      setToken(result.token, result.expire_at);
      navigate(destination, { replace: true });
    } finally {
      setLoading(false);
    }
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
            从灵感到画面，<br />
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
          <p className="text-muted-foreground mb-2 text-sm">欢迎回来</p>
          <h2 className="text-3xl font-semibold tracking-tight">登录你的账号</h2>
          <p className="text-muted-foreground mt-3">输入用户名和密码，继续使用画布。</p>

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
              <div className="relative">
                <Input
                  id="password"
                  name="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="current-password"
                  required
                  minLength={6}
                  maxLength={128}
                  placeholder="请输入密码"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  disabled={loading}
                  className="h-11 pr-11"
                />
                <button
                  type="button"
                  aria-label={showPassword ? "隐藏密码" : "显示密码"}
                  aria-pressed={showPassword}
                  onClick={() => setShowPassword((value) => !value)}
                  className="text-muted-foreground hover:text-foreground absolute top-1/2 right-3 -translate-y-1/2 rounded p-1 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              </div>
            </div>

            <Button type="submit" size="lg" disabled={loading} className="h-11 w-full justify-center">
              {loading ? (
                <><LoaderCircle className="size-4 animate-spin" /> 登录中...</>
              ) : (
                <>登录 <ArrowRight className="size-4" /></>
              )}
            </Button>
          </form>
        </div>
      </section>
    </main>
  );
}
