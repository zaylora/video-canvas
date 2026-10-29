import { motion, useReducedMotion } from "motion/react";

import { DURATION, EASE_OUT, STAGGER } from "@/lib/motion";

/** 标题里带琥珀流光的词 */
const HIGHLIGHT = "创作";

/** 每个字入场时从下方升起的距离（px） */
const RISE = 8;

/**
 * 创作页的问候标题：每个字依次从下方上浮并淡入，间隔 STAGGER，只动 transform 与 opacity。
 * 「创作」两个字作为一个整体入场（占两个字的节拍）：渐变文字靠 background-clip: text，
 * 裁不进各自做了 transform 的子元素，拆成单字会让整块消失。它入场后先扫一遍光，随后在琥珀色之间缓慢流动（home-glow-text）。
 * 读屏器读整句，不读一个个字；系统开了「减少动态效果」时只淡入，不位移，也不流光。
 */
export function GreetingTitle({ hello }: { hello: string }) {
  const reduced = useReducedMotion();
  const lead = [...`${hello}，今天想要`];
  const tail = [...HIGHLIGHT];
  const end = [..."什么？"];

  /** 第 index 个节拍的入场参数 */
  const enter = (index: number) => ({
    initial: { opacity: 0, y: reduced ? 0 : RISE },
    animate: { opacity: 1, y: 0 },
    transition: { duration: DURATION.slow, ease: EASE_OUT, delay: index * STAGGER },
  });

  return (
    <h1
      aria-label={`${hello}，今天想要${HIGHLIGHT}什么？`}
      className="font-display text-[26px] font-black tracking-wide md:text-[34px]"
    >
      <span aria-hidden>
        {lead.map((char, i) => (
          <motion.span key={`lead-${i}`} className="inline-block" {...enter(i)}>
            {char}
          </motion.span>
        ))}
        <motion.span className="home-glow-text inline-block" {...enter(lead.length)}>
          {HIGHLIGHT}
        </motion.span>
        {end.map((char, i) => (
          <motion.span
            key={`end-${i}`}
            className="inline-block"
            {...enter(lead.length + tail.length + i)}
          >
            {char}
          </motion.span>
        ))}
      </span>
    </h1>
  );
}
