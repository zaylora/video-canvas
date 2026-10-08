import { useEffect, useState } from "react";
import { motion } from "motion/react";
import { Quote, WandSparkles } from "lucide-react";

import type { ShowcaseItemDto } from "@/api/showcase/type";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";

/** 逐字打出提示词的间隔（毫秒） */
const TYPE_INTERVAL_MS = 32;

/** 逐字打出一句话；instant 时（减少动态效果）直接显示整句。换作品时由外层用 key 重新挂载 */
function TypedPrompt({ text, instant }: { text: string; instant: boolean }) {
  const [length, setLength] = useState(instant ? text.length : 0);

  useEffect(() => {
    if (instant) return;
    const timer = window.setInterval(() => {
      setLength((value) => {
        if (value >= text.length) {
          window.clearInterval(timer);
          return value;
        }
        return value + 1;
      });
    }, TYPE_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [text, instant]);

  const done = length >= text.length;
  return (
    <p
      aria-label={text}
      className="mt-1.5 min-h-[1.55em] text-[15px] leading-[1.55] text-pretty [text-shadow:0_1px_12px_oklch(0_0_0/0.5)] max-md:text-sm"
    >
      <span aria-hidden>
        “{text.slice(0, length)}
        {done ? (
          "”"
        ) : (
          <i className="ml-0.5 inline-block h-[1.05em] w-[1.5px] animate-[showcase-caret_1s_steps(1)_infinite] bg-current align-[-2px]" />
        )}
      </span>
    </p>
  );
}

/**
 * 登录页左下角的字幕：「这段画面，由一句话生成」+ 生成它的那句话 + 模型标签 +「做同款」。
 * 切条时淡入并逐字打出提示词；减少动态效果时直接显示整句。
 * @param item 当前作品
 * @param instant 是否跳过逐字动画
 * @param onSame 点「做同款」，带上这条提示词和被点的按钮（抽屉关闭后把焦点还给它）
 */
export function ShowcaseCaption({
  item,
  instant,
  onSame,
}: {
  item: ShowcaseItemDto;
  instant: boolean;
  onSame: (prompt: string, opener: HTMLElement) => void;
}) {
  return (
    <motion.div
      key={item.id}
      data-slot="showcase-caption"
      initial={instant ? false : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="text-on-stage max-w-[520px] min-w-0 max-md:max-w-none"
    >
      <div className="text-on-stage-muted flex items-center gap-1.5 text-[11px] tracking-[0.06em]">
        <Quote className="size-3" />
        这段画面，由一句话生成
      </div>
      <TypedPrompt key={item.id} text={item.prompt} instant={instant} />
      <div className="text-on-stage-muted mt-2.5 flex items-center gap-2.5 text-xs">
        {item.modelLabel && <span>{item.modelLabel}</span>}
        <motion.button
          type="button"
          whileTap={TAP}
          onClick={(event) => onSame(item.prompt, event.currentTarget)}
          className="bg-stage-glass border-stage-glass-border text-on-stage hover:bg-stage-glass-border focus-visible:ring-on-stage/60 inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-xs outline-none backdrop-blur-md focus-visible:ring-2"
        >
          <WandSparkles className="size-3.5" />
          做同款
        </motion.button>
      </div>
    </motion.div>
  );
}
