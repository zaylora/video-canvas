import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { motion } from "motion/react";
import { ArrowRight, Sparkle, X } from "lucide-react";

import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { carryLabel } from "@/utils/login/word-swap";

/** 胶囊的高度和宽度上限；卡片的宽度上限 */
const CAPSULE_HEIGHT = 62;
const CAPSULE_WIDTH = 600;
const CARD_WIDTH = 420;
/** 两侧至少留的空白 */
const GUTTER = 32;
/** 胶囊展开成卡片的时长，和外框的过渡一致 */
const MORPH_MS = 440;

/** 读当前视口宽度；服务端渲染（测试）时按桌面宽度算 */
const viewportWidth = () => (typeof window === "undefined" ? 1280 : window.innerWidth);

/**
 * 登录页中间的胶囊 ⇄ 卡片（设计稿：登录页原型）：
 * 收着时是一颗半透明胶囊，写一句想法，点「开始创作」；它原地展开成登录 / 注册卡片（宽、高、圆角一起过渡，440ms 抽屉曲线），
 * 写过的想法显示在卡片顶部「登录后继续：…」，登录后带到创作页。Esc 或右上角关闭，收回成胶囊并把焦点还给输入框。
 * 外框只有一个元素，卡片高度用 ResizeObserver 跟着内容（切换登录 / 注册、出错提示）平滑变化；
 * 胶囊和卡片两层叠放交叉淡入，没显示的那层 inert。卡片固定用深色（舞台永远是深色），不跟全站主题。
 * @param open 是否展开成卡片
 * @param idea 胶囊里的想法（受控）
 * @param title 卡片标题
 * @param onStart 点「开始创作」（或在胶囊里回车）
 * @param onClose 关闭卡片
 * @param children 卡片里的表单
 */
export function IdeaDock({
  open,
  idea,
  title,
  onIdeaChange,
  onStart,
  onClose,
  children,
}: {
  open: boolean;
  idea: string;
  title: string;
  onIdeaChange: (value: string) => void;
  onStart: () => void;
  onClose: () => void;
  children: ReactNode;
}) {
  const titleId = useId();
  const cardRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [cardHeight, setCardHeight] = useState(0);
  const [width, setWidth] = useState(viewportWidth);
  const carry = carryLabel(idea);

  useEffect(() => {
    const card = cardRef.current;
    if (!card) return;
    const observer = new ResizeObserver(() => setCardHeight(card.offsetHeight));
    observer.observe(card);
    const onResize = () => setWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", onResize);
    };
  }, []);

  // 展开后把焦点交给卡片里第一个输入框；收起后还给胶囊的输入框
  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (open) cardRef.current?.querySelector<HTMLInputElement>("input")?.focus();
      else inputRef.current?.focus({ preventScroll: true });
    }, MORPH_MS);
    return () => window.clearTimeout(timer);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open, onClose]);

  const cardWidth = Math.min(CARD_WIDTH, width - GUTTER);
  const capsuleWidth = Math.min(CAPSULE_WIDTH, width - GUTTER);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onStart();
  };

  return (
    <div
      data-slot="idea-dock"
      data-open={open}
      className="relative overflow-hidden bg-[oklch(0.17_0_0/0.58)] shadow-[0_24px_60px_-24px_oklch(0_0_0/0.75)] ring-1 ring-white/12 backdrop-blur-2xl backdrop-saturate-150 transition-[width,height,border-radius] duration-[440ms,320ms,440ms] ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none"
      style={{
        width: open ? cardWidth : capsuleWidth,
        height: open && cardHeight ? cardHeight : CAPSULE_HEIGHT,
        borderRadius: open ? 28 : CAPSULE_HEIGHT / 2,
      }}
    >
      <form
        data-slot="idea-capsule"
        inert={open || undefined}
        onSubmit={submit}
        className={cn(
          "absolute inset-x-0 top-0 flex items-center gap-1.5 py-1.5 pr-1.5 pl-6 transition-opacity duration-160",
          open && "opacity-0",
        )}
        style={{ height: CAPSULE_HEIGHT }}
      >
        <input
          ref={inputRef}
          value={idea}
          onChange={(event) => onIdeaChange(event.target.value)}
          placeholder="写下脑海里的画面…"
          aria-label="写下脑海里的画面"
          className="text-on-stage placeholder:text-on-stage-muted min-w-0 flex-1 bg-transparent text-[15.5px] outline-none"
        />
        <motion.button
          type="submit"
          whileTap={TAP}
          className="group/cta bg-on-stage text-stage focus-visible:ring-on-stage/60 inline-flex h-full items-center gap-2 rounded-full px-6 text-[15px] font-semibold outline-none hover:opacity-90 focus-visible:ring-2"
        >
          <span className="max-sm:hidden">开始创作</span>
          <ArrowRight className="size-4 transition-transform duration-200 group-hover/cta:translate-x-0.5 motion-reduce:group-hover/cta:translate-x-0" />
        </motion.button>
      </form>

      <div
        ref={cardRef}
        role="dialog"
        aria-modal={open || undefined}
        aria-labelledby={titleId}
        aria-hidden={!open || undefined}
        inert={!open || undefined}
        className={cn(
          "dark text-foreground absolute top-0 left-0 p-6 transition-opacity duration-160",
          open ? "opacity-100 delay-160 duration-260" : "opacity-0",
        )}
        style={{ width: cardWidth }}
      >
        <div className="flex items-center justify-between">
          <h2 id={titleId} className="text-xl font-semibold tracking-tight">
            {title}
          </h2>
          <motion.button
            type="button"
            whileTap={TAP}
            onClick={onClose}
            aria-label="关闭（Esc）"
            title="关闭（Esc）"
            className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 grid size-8 place-items-center rounded-full outline-none focus-visible:ring-3"
          >
            <X className="size-4" />
          </motion.button>
        </div>
        {carry && (
          <p className="bg-chrome-hover text-muted-foreground mt-3 flex items-center gap-2 rounded-2xl px-3.5 py-2.5 text-[13px]">
            <Sparkle className="text-beam-bright fill-beam-bright size-3.5 shrink-0" />
            <span className="truncate">{carry}</span>
          </p>
        )}
        {children}
      </div>
    </div>
  );
}
