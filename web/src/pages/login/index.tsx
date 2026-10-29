import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Clapperboard } from "lucide-react";
import { toast } from "sonner";

import { getAuthConfig, login, type AuthConfig } from "@/api/auth";
import { Logo } from "@/components/brand/logo";
import { ShowcaseCaption } from "@/components/showcase/showcase-caption";
import { ShowcaseLayers } from "@/components/showcase/showcase-layers";
import { ShowcaseProgress } from "@/components/showcase/showcase-progress";
import { usePageTitle } from "@/hooks/use-page-title";
import { useShowcase } from "@/hooks/use-showcase";
import { useShowcasePlayer } from "@/hooks/use-showcase-player";
import type { ShowcaseItemDto } from "@/api/showcase/type";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { mapAuthError } from "@/utils/auth/register-rules";
import { saveCarry } from "@/utils/home/carry";
import { ApiError } from "@/utils/requests/request";
import { formatPageTitle } from "@/utils/page-title";
import { getToken, setToken } from "@/utils/storage/token";

import { AuthPanel, type AuthMode } from "./auth-panel";
import { IdeaDock } from "./idea-dock";
import { LoginStage } from "./login-stage";
import { WordSwapTitle } from "./word-swap-title";

/** 还没取到作品时给播放器的空列表：用常量是为了引用稳定，别让播放器每次渲染都重算 */
const NO_ITEMS: ShowcaseItemDto[] = [];

/** 品牌主 Slogan，登录页顶栏用 */
const BRAND_SLOGAN = "意义，在镜头之间。";

/** 登录成功后「欢迎」过渡停留多久再进首页 */
const ENTER_MS = 700;

function getDestination(search: string) {
  const next = new URLSearchParams(search).get("next");
  return next && next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/login")
    ? next
    : "/";
}

/**
 * 登录页（设计稿 docs/品牌包装/登录与首页改版原型）：整屏是一块深色舞台，
 * 顶栏是连镜 Logo 和 Slogan，中间是「从一句话，到一部片。」（末尾的词轮换）和一颗写想法的胶囊；
 * 点「开始创作」胶囊原地展开成登录 / 注册卡片，写下的想法登录后带到创作页。成功后黑场里亮出 Logo 和欢迎语，再进首页。
 * 保留 ?next= 跳转和 ?tab=register；带着 next 或 tab 来的（被拦截到登录页、点了注册链接）直接展开卡片。
 * 进页面先取 /auth/config：注册关闭时不显示注册入口；无需验证码时（首个账号）隐藏验证码。
 */
export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  usePageTitle(formatPageTitle("登录"));
  const params = new URLSearchParams(location.search);
  const destination = getDestination(location.search);
  const wantsRegister = params.get("tab") === "register";
  const [config, setConfig] = useState<AuthConfig | null>(null);
  const [cardOpen, setCardOpen] = useState(wantsRegister || params.has("next"));
  const [mode, setMode] = useState<AuthMode>(wantsRegister ? "register" : "login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [loginError, setLoginError] = useState("");
  /** 胶囊里写的想法（或「做同款」填进来的提示词）：登录或注册成功后记进 sessionStorage，创作页读走 */
  const [idea, setIdea] = useState("");
  /** 登录成功后的欢迎语；有值时盖上进入过渡，随后跳转 */
  const [welcome, setWelcome] = useState("");
  const reducedMotion = useReducedMotion();
  const showcase = useShowcase();
  const player = useShowcasePlayer(
    showcase?.items ?? NO_ITEMS,
    showcase?.settings.clipSeconds ?? 7,
    showcase?.settings.posterOnSaveData ?? true,
  );
  const enterTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(enterTimer.current), []);

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

  const closeCard = useCallback(() => setCardOpen(false), []);

  if (getToken() && !welcome) return <Navigate to={destination} replace />;

  const registerOpen = config?.register_enabled === true;
  const registerClosedHint = wantsRegister && config !== null && !registerOpen;
  const activeMode: AuthMode = mode === "register" && registerOpen ? "register" : "login";

  /** 展开卡片：开始创作优先引到注册（注册开着时），顶栏「登录」永远是登录 */
  const openCard = (target: AuthMode) => {
    setMode(target);
    setLoginError("");
    setCardOpen(true);
  };

  /** 登录 / 注册成功：带上想法，盖上欢迎过渡，稍后进首页（减少动态效果时立刻进） */
  const enter = (name: string) => {
    saveCarry(idea.trim());
    setWelcome(name ? `欢迎，${name}` : "欢迎来到连镜");
    enterTimer.current = window.setTimeout(
      () => navigate(destination, { replace: true }),
      reducedMotion ? 0 : ENTER_MS,
    );
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (loading) return;
    setLoading(true);
    setLoginError("");

    try {
      const result = await login({ username: username.trim(), password });
      setToken(result.token, result.expire_at, result.role);
      enter(username.trim());
    } catch (error) {
      const hint = error instanceof ApiError ? mapAuthError(error.code) : null;
      if (hint?.field === "form") setLoginError(hint.message);
    } finally {
      setLoading(false);
    }
  };

  const handleRegistered = (firstUser: boolean) => {
    if (firstUser) toast.success("你是首个用户，已成为超级管理员，请到后台配置邮件服务");
    enter("");
  };

  /** 点字幕里的「做同款」：把这条提示词填进胶囊并展开卡片，注册开着引到注册 */
  const handleSame = (prompt: string) => {
    setIdea(prompt);
    openCard(registerOpen ? "register" : "login");
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
            onClick={() => openCard("login")}
            className="hover:bg-stage-glass focus-visible:ring-on-stage/60 h-9 rounded-full px-3.5 outline-none focus-visible:ring-2"
          >
            登录
          </motion.button>
          {registerOpen && (
            <motion.button
              type="button"
              whileTap={TAP}
              onClick={() => openCard("register")}
              className="bg-stage-glass border-stage-glass-border hover:bg-stage-glass/80 focus-visible:ring-on-stage/60 h-9 rounded-full border px-4 font-medium backdrop-blur-xl outline-none focus-visible:ring-2 max-md:hidden"
            >
              免费注册
            </motion.button>
          )}
        </div>
      </header>

      {/* 卡片展开时标题上移、缩小，副标题和角标淡出，把位置让给卡片 */}
      <div className="absolute inset-0 z-[5] flex flex-col items-center justify-center px-4 pb-[10svh]">
        <span
          className={cn(
            "bg-stage-glass border-stage-glass-border text-on-stage-muted inline-flex h-7 items-center gap-1.5 rounded-full border px-3 text-xs tracking-[0.12em] backdrop-blur-md transition-opacity duration-200",
            cardOpen && "pointer-events-none opacity-0",
          )}
        >
          <Clapperboard className="size-3.5" />
          AI 视频创作画布
        </span>
        <WordSwapTitle
          className={cn(
            "mt-5 text-center text-[clamp(40px,5.8vw,78px)] leading-[1.06] font-semibold tracking-[-0.035em] [text-shadow:0_2px_30px_oklch(0_0_0/0.35)]",
            "transition-transform duration-[440ms] ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none",
            cardOpen && "-translate-y-3 scale-[0.86]",
          )}
        />
        <p
          className={cn(
            "text-on-stage-muted mx-auto mt-4 max-w-[560px] text-center text-[15px] leading-relaxed transition-opacity duration-200 md:text-[16.5px]",
            cardOpen && "pointer-events-none opacity-0",
          )}
        >
          写下脑海里的画面，AI 帮你拆分镜、生成镜头，在一张画布上连成故事。
        </p>
        <div className="mt-9">
          <IdeaDock
            open={cardOpen}
            idea={idea}
            title={activeMode === "login" ? "登录" : "加入连镜"}
            onIdeaChange={setIdea}
            onStart={() => openCard(registerOpen ? "register" : "login")}
            onClose={closeCard}
          >
            <AuthPanel
              mode={activeMode}
              config={config}
              registerOpen={registerOpen}
              registerClosedHint={registerClosedHint}
              username={username}
              password={password}
              loading={loading}
              loginError={loginError}
              onModeChange={setMode}
              onUsernameChange={setUsername}
              onPasswordChange={setPassword}
              onSubmit={handleSubmit}
              onRegistered={handleRegistered}
            />
          </IdeaDock>
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
              cardOpen && "pointer-events-none opacity-0",
            )}
          />
        </footer>
      )}

      {/* 进入过渡：黑场里亮出 Logo 两笔和欢迎语，随后跳到首页 */}
      <AnimatePresence>
        {welcome && (
          <motion.div
            key="enter"
            role="status"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: DURATION.slow, ease: EASE_OUT }}
            className="bg-stage fixed inset-0 z-50 flex flex-col items-center justify-center gap-4"
          >
            <motion.div
              initial={{ opacity: 0, scale: reducedMotion ? 1 : 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={{ duration: 0.5, ease: EASE_OUT, delay: 0.1 }}
            >
              <Logo size={52} bright />
            </motion.div>
            <motion.p
              initial={{ opacity: 0, y: reducedMotion ? 0 : 6 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.4, ease: EASE_OUT, delay: 0.25 }}
              className="text-xl font-semibold tracking-tight"
            >
              {welcome}
            </motion.p>
          </motion.div>
        )}
      </AnimatePresence>
    </main>
  );
}
