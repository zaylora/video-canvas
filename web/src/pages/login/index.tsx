import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { motion } from "motion/react";
import { ArrowRight, Clapperboard } from "lucide-react";
import { toast } from "sonner";

import { getAuthConfig, login, type AuthConfig } from "@/api/auth";
import { Logo } from "@/components/brand/logo";
import { TypewriterTitle } from "@/components/brand/typewriter-title";
import { ShowcaseCaption } from "@/components/showcase/showcase-caption";
import { ShowcaseLayers } from "@/components/showcase/showcase-layers";
import { ShowcaseProgress } from "@/components/showcase/showcase-progress";
import { useShowcase } from "@/hooks/use-showcase";
import { useShowcasePlayer } from "@/hooks/use-showcase-player";
import type { ShowcaseItemDto } from "@/api/showcase/type";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { mapAuthError } from "@/utils/auth/register-rules";
import { saveCarry } from "@/utils/home/carry";
import { ApiError } from "@/utils/requests/request";
import { getToken, setToken } from "@/utils/storage/token";

import { AuthDrawer, type AuthMode } from "./auth-drawer";
import { LoginStage } from "./login-stage";

/** 还没取到作品时给播放器的空列表：用常量是为了引用稳定，别让播放器每次渲染都重算 */
const NO_ITEMS: ShowcaseItemDto[] = [];

/** 品牌主 Slogan，登录页顶栏用 */
const BRAND_SLOGAN = "意义，在镜头之间。";

/** 登录页大标题：开场打出「从一句话，到一部片。」，之后把「片」删掉换成「电影」，打完就停；宽屏一行，窄屏在逗号后换行 */
const HERO_TITLE = { lead: "从一句话，", stem: "到一部", words: ["片", "电影"], end: "。" };

function getDestination(search: string) {
  const next = new URLSearchParams(search).get("next");
  return next && next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/login")
    ? next
    : "/";
}

/**
 * 登录页（设计稿 docs/品牌包装/登录与首页改版原型）：整屏是一块深色舞台，
 * 顶栏是连镜 Logo 和 Slogan，中间是「从一句话，到一部片。」和「开始创作」，点开始创作从右侧滑出登录 / 注册抽屉。
 * 保留 ?next= 跳转和 ?tab=register；带着 next 或 tab 来的（被拦截到登录页、点了注册链接）直接展开抽屉。
 * 进页面先取 /auth/config：注册关闭时不显示注册入口；无需验证码时（首个账号）隐藏验证码。
 */
export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  const params = new URLSearchParams(location.search);
  const destination = getDestination(location.search);
  const wantsRegister = params.get("tab") === "register";
  const [config, setConfig] = useState<AuthConfig | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(wantsRegister || params.has("next"));
  const [mode, setMode] = useState<AuthMode>(wantsRegister ? "register" : "login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [loginError, setLoginError] = useState("");
  /** 「做同款」带来的提示词：登录或注册成功后记进 sessionStorage，首页读走 */
  const [carry, setCarry] = useState("");
  const showcase = useShowcase();
  const player = useShowcasePlayer(
    showcase?.items ?? NO_ITEMS,
    showcase?.settings.clipSeconds ?? 7,
    showcase?.settings.posterOnSaveData ?? true,
  );
  /** 打开抽屉的那个按钮，关闭后把焦点还给它 */
  const openerRef = useRef<HTMLElement | null>(null);

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

  const closeDrawer = useCallback(() => {
    setDrawerOpen(false);
    openerRef.current?.focus();
  }, []);

  if (getToken()) return <Navigate to={destination} replace />;

  const registerOpen = config?.register_enabled === true;
  const registerClosedHint = wantsRegister && config !== null && !registerOpen;
  const activeMode: AuthMode = mode === "register" && registerOpen ? "register" : "login";

  /** 打开抽屉：开始创作优先引到注册（注册开着时），顶栏「登录」永远是登录 */
  const openDrawer = (target: AuthMode, opener: HTMLElement) => {
    openerRef.current = opener;
    setMode(target);
    setLoginError("");
    setDrawerOpen(true);
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (loading) return;
    setLoading(true);
    setLoginError("");

    try {
      const result = await login({ username: username.trim(), password });
      setToken(result.token, result.expire_at, result.role);
      saveCarry(carry);
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
    saveCarry(carry);
    navigate(destination, { replace: true });
  };

  /** 点字幕里的「做同款」：带着这条提示词打开抽屉，注册开着引到注册 */
  const handleSame = (prompt: string, opener: HTMLElement) => {
    setCarry(prompt);
    openDrawer(registerOpen ? "register" : "login", opener);
  };

  return (
    <main className="bg-stage text-on-stage relative h-svh min-h-[620px] overflow-hidden">
      <LoginStage>{player.count > 0 && <ShowcaseLayers player={player} />}</LoginStage>

      <header className="absolute inset-x-0 top-0 z-10 flex h-16 items-center justify-between px-4 md:h-18 md:px-8">
        <div className="flex items-center gap-2.5 text-lg font-semibold tracking-wide">
          <Logo size={32} bright />
          连镜
          <span className="border-stage-glass-border text-on-stage-muted ml-1 border-l pl-3 text-[13px] font-normal tracking-normal max-md:hidden">
            {BRAND_SLOGAN}
          </span>
        </div>
        <div className="flex gap-2">
          <motion.button
            type="button"
            whileTap={TAP}
            onClick={(event) => openDrawer("login", event.currentTarget)}
            className="hover:bg-stage-glass focus-visible:ring-on-stage/60 h-9 rounded-full px-3.5 outline-none focus-visible:ring-2"
          >
            登录
          </motion.button>
          {registerOpen && (
            <motion.button
              type="button"
              whileTap={TAP}
              onClick={(event) => openDrawer("register", event.currentTarget)}
              className="bg-on-stage text-stage focus-visible:ring-on-stage/60 h-9 rounded-full px-4 font-medium outline-none hover:opacity-90 focus-visible:ring-2 max-md:hidden"
            >
              免费注册
            </motion.button>
          )}
        </div>
      </header>

      {/* 抽屉打开且屏幕够宽时，主文案向左让出 210px，背景和文案都还看得见 */}
      <div className="absolute inset-0 z-[5] flex items-center justify-center px-4 pb-[8svh]">
        <div
          data-shift={drawerOpen || undefined}
          className={cn(
            "w-[min(1040px,100%)] text-center transition-transform duration-240 ease-[cubic-bezier(0.2,0,0,1)] motion-reduce:transition-none",
            "min-[1180px]:data-shift:-translate-x-[210px]",
          )}
        >
          <span className="bg-stage-glass border-stage-glass-border text-on-stage-muted inline-flex h-7 items-center gap-1.5 rounded-full border px-3 text-xs backdrop-blur-md">
            <Clapperboard className="size-3.5" />
            AI 视频创作画布
          </span>
          <TypewriterTitle
            {...HERO_TITLE}
            className="mt-5 text-[clamp(40px,6.2vw,80px)] leading-[1.04] font-semibold tracking-[-0.035em] [text-shadow:0_2px_30px_oklch(0_0_0/0.35)]"
          />
          <p className="text-on-stage-muted mx-auto mt-4.5 max-w-[520px] text-[15px] leading-relaxed md:max-w-none md:text-[17px] md:whitespace-nowrap">
            写下脑海里的画面，AI 帮你拆分镜、生成镜头，在一张画布上连成故事。
          </p>
          <motion.button
            type="button"
            whileTap={TAP}
            onClick={(event) =>
              openDrawer(registerOpen ? "register" : "login", event.currentTarget)
            }
            className="group/cta bg-on-stage text-stage focus-visible:ring-on-stage/60 mt-8.5 inline-flex h-13 items-center gap-2 rounded-full px-6.5 text-base font-semibold shadow-[0_18px_50px_-20px_oklch(0_0_0/0.7)] outline-none hover:opacity-90 focus-visible:ring-2"
          >
            开始创作
            <ArrowRight className="size-4 transition-transform duration-120 group-hover/cta:translate-x-0.5 motion-reduce:group-hover/cta:translate-x-0" />
          </motion.button>
        </div>
      </div>

      {player.current && (
        <footer className="absolute inset-x-4 bottom-5 z-[5] flex flex-col items-stretch gap-3.5 md:inset-x-8 md:bottom-7 md:flex-row md:items-end md:justify-between md:gap-6">
          <ShowcaseCaption
            item={player.current}
            instant={player.reducedMotion}
            onSame={handleSame}
          />
          <ShowcaseProgress
            player={player}
            showPause={false}
            className={cn(
              "transition-opacity duration-180",
              drawerOpen && "pointer-events-none opacity-0",
            )}
          />
        </footer>
      )}

      <AuthDrawer
        open={drawerOpen}
        mode={activeMode}
        config={config}
        registerOpen={registerOpen}
        registerClosedHint={registerClosedHint}
        username={username}
        password={password}
        loading={loading}
        loginError={loginError}
        onClose={closeDrawer}
        onModeChange={setMode}
        onUsernameChange={setUsername}
        onPasswordChange={setPassword}
        onSubmit={handleSubmit}
        onRegistered={handleRegistered}
      />
    </main>
  );
}
