import { useState } from "react";
import { Link } from "react-router";
import { AnimatePresence, motion } from "motion/react";
import { ArrowUp, ChevronRight, Search, Sparkles } from "lucide-react";

import { BannerCarousel } from "@/components/home/banner-carousel";
import {
  CanvasCard,
  CanvasCardSkeleton,
  CardListNote,
  NewCanvasCard,
} from "@/components/home/canvas-card";
import { Segmented } from "@/components/home/segmented";
import { SoonTip } from "@/components/home/soon";
import { WorksSection } from "@/components/home/works-section";
import { Button } from "@/components/ui/button";
import { useCanvasList } from "@/hooks/use-canvas-list";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { useUsername } from "@/pages/canvas/chrome/top-right-bar";
import { greeting } from "@/utils/home/home";

/** 首屏最多显示几张最近画布，加上新建卡正好一行 6 格 */
const RECENT_COUNT = 5;

/** 快捷提示：点了把文案填进输入框，目前不会提交 */
const PROMPT_CHIPS: { label: string; text: string }[] = [
  { label: "分镜脚本", text: "把这段故事拆成 6 个分镜：" },
  { label: "角色设定", text: "设计一个角色：" },
  { label: "产品广告", text: "为这款产品做 15 秒广告：" },
  { label: "画面风格", text: "统一画面风格为：" },
  { label: "AIMV", text: "根据这首歌的歌词生成 MV 分镜：" },
];

type Scope = "all" | "mine" | "shared";

/** 输入创意的大输入框：目前只做外观，发送按钮禁用 */
function IdeaPrompt() {
  const [idea, setIdea] = useState("");

  return (
    <>
      <label className="bg-background/70 ring-border focus-within:ring-ring mx-auto mt-6 flex h-14 max-w-3xl items-center gap-2.5 rounded-2xl pr-2.5 pl-4 ring-1 transition-[box-shadow,transform] duration-120 focus-within:-translate-y-px focus-within:shadow-xl motion-reduce:focus-within:translate-y-0">
        <Sparkles className="text-muted-foreground size-4 shrink-0" />
        <input
          value={idea}
          onChange={(event) => setIdea(event.target.value)}
          placeholder="说出你的创意，在画布上实现 …"
          className="placeholder:text-muted-foreground min-w-0 flex-1 bg-transparent text-[15px] outline-none"
        />
        <SoonTip>
          <Button disabled size="icon" aria-label="发送" className="rounded-full">
            <ArrowUp />
          </Button>
        </SoonTip>
      </label>
      <div className="mt-3.5 flex gap-2 overflow-x-auto md:flex-wrap md:justify-center">
        {PROMPT_CHIPS.map((chip) => (
          <motion.button
            key={chip.label}
            type="button"
            whileTap={TAP}
            onClick={() => setIdea(chip.text)}
            className="bg-muted/60 text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 h-8 shrink-0 rounded-full px-3 text-xs outline-none transition-colors duration-150 focus-visible:ring-3"
          >
            {chip.label}
          </motion.button>
        ))}
      </div>
    </>
  );
}

/**
 * 首页（设计稿 docs/design/首页）：顺序是 创作 → 继续 → 浏览。
 * Hero 面板里是问候、大输入框、最近画布；下面是运营 Banner 和作品广场（都是占位）。
 */
export default function Home() {
  const username = useUsername();
  /** 问候语只在进入页面时算一次 */
  const [hello] = useState(() => greeting(new Date().getHours()));
  const [scope, setScope] = useState<Scope>("mine");
  const list = useCanvasList(RECENT_COUNT);
  const recent = list.items.slice(0, RECENT_COUNT);

  let body;
  if (list.loading) {
    body = Array.from({ length: RECENT_COUNT }, (_, i) => (
      <CanvasCardSkeleton key={i} variant="overlay" />
    ));
  } else if (list.error) {
    body = (
      <CardListNote key="note" className="col-span-2 md:col-span-5">
        加载失败
        <Button variant="outline" size="sm" onClick={list.reload}>
          重试
        </Button>
      </CardListNote>
    );
  } else if (recent.length === 0) {
    body = (
      <CardListNote key="note" className="col-span-2 md:col-span-5">
        {list.keyword ? (
          <>
            没有找到“{list.keyword}”
            <Button variant="link" size="sm" onClick={() => list.setQuery("")}>
              清空搜索
            </Button>
          </>
        ) : (
          "还没有画布，从这里开始"
        )}
      </CardListNote>
    );
  } else {
    body = recent.map((canvas, index) => (
      <CanvasCard
        key={canvas.id}
        canvas={canvas}
        variant="overlay"
        index={index}
        deleting={list.deleting.has(canvas.id)}
        onDelete={() => void list.remove(canvas.id)}
      />
    ));
  }

  return (
    <div className="mx-auto w-full max-w-[1200px] px-4 pt-2 pb-24 md:px-6 md:pt-4">
      <motion.section
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: DURATION.slow, ease: EASE_OUT }}
        className="bg-card/60 ring-border relative overflow-hidden rounded-[22px] px-4 pt-8 pb-6 ring-1 md:rounded-[28px] md:px-12 md:pt-14 md:pb-10"
      >
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle,var(--canvas-dot)_1px,transparent_1.3px)] bg-size-[20px_20px] mask-[radial-gradient(ellipse_70%_60%_at_50%_30%,black_30%,transparent_80%)]"
        />
        <div className="relative">
          <h1 className="text-center text-2xl tracking-tight md:text-4xl">
            <span className="text-muted-foreground font-light">{hello}，</span>
            <span className="font-semibold">{username ?? "欢迎回来"}</span>
          </h1>
          <IdeaPrompt />

          <div className="mt-8 mb-3.5 flex items-center gap-2 md:mt-10">
            <Segmented<Scope>
              value={scope}
              onChange={setScope}
              options={[
                { value: "all", label: "全部" },
                { value: "mine", label: "个人" },
                { value: "shared", label: "协作", soon: true },
              ]}
            />
            <div className="flex-1" />
            <label className="bg-muted/60 text-muted-foreground focus-within:ring-ring flex h-8 w-30 items-center gap-1.5 rounded-[10px] px-2.5 ring-0 transition-shadow focus-within:ring-1 md:w-48">
              <Search className="size-4 shrink-0" />
              <input
                value={list.query}
                onChange={(event) => list.setQuery(event.target.value)}
                onKeyDown={(event) => event.key === "Escape" && list.setQuery("")}
                placeholder="搜索"
                aria-label="搜索画布"
                className="text-foreground min-w-0 flex-1 bg-transparent outline-none"
              />
            </label>
            <Link
              to="/canvases"
              className="group/more text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex shrink-0 items-center gap-0.5 rounded-md px-1.5 py-1 whitespace-nowrap outline-none focus-visible:ring-3"
            >
              所有画布
              <ChevronRight className="size-4 transition-transform duration-120 group-hover/more:translate-x-0.5 motion-reduce:group-hover/more:translate-x-0" />
            </Link>
          </div>

          <div className="grid auto-cols-[42%] grid-flow-col gap-3 overflow-x-auto pb-1 md:auto-cols-[calc((100%-5*12px)/6)]">
            <NewCanvasCard
              variant="overlay"
              creating={list.creating}
              onCreate={() => void list.create()}
            />
            <AnimatePresence initial={false}>{body}</AnimatePresence>
          </div>
        </div>
      </motion.section>

      <div className="mt-6">
        <BannerCarousel />
      </div>
      <WorksSection />
    </div>
  );
}
