import { motion, useReducedMotion } from "motion/react";

import { useTypewriter } from "@/hooks/use-typewriter";
import { DURATION } from "@/lib/motion";
import type { TypewriterTiming } from "@/utils/login/typewriter";

/** 节奏：打字一个字 110ms，删得比打快，停留够读完，删光后空一下再打下一个词 */
const TIMING: TypewriterTiming = {
  delay: 250,
  typeInterval: 110,
  deleteInterval: 60,
  hold: 2600,
  pause: 300,
};

interface TypewriterTitleProps {
  /** 前半句：宽屏接成一行，窄屏单独占一行 */
  lead: string;
  /** 后半句里不变的前缀，词接在它后面 */
  stem: string;
  /** 依次出现的词：开场打出第一个，之后删掉换下一个，最后一个打完就停 */
  words: string[];
  /** 词后面的标点，一直都在 */
  end: string;
  className?: string;
}

const chars = (text: string) => [...text];

/**
 * 登录页大标题：开场把整句打出来，之后只把最后一个词删掉、换成下一个，最后一个词打完就停下，不循环。
 * 版面按最长的词留好宽度、整块居中，换词时句子不会左右晃；光标跟在词后面，
 * 打字删字时常亮，停留与空档时闪烁，打完停下后淡出。读屏器读完整句子，不读一个个字。
 * 系统开了「减少动态效果」时直接静止显示最后一个词的完整句子，不要光标。
 */
export function TypewriterTitle({ lead, stem, words, end, className }: TypewriterTitleProps) {
  const reduced = useReducedMotion() ?? false;
  const prefixLength = chars(lead + stem).length;
  const frame = useTypewriter(prefixLength, words, TIMING, reduced);

  const leadChars = chars(lead);
  const leadShown = Math.min(frame.prefix, leadChars.length);
  const stemShown = frame.prefix - leadShown;
  const word = chars(words[frame.word] ?? "").slice(0, frame.count);
  const widest = words.reduce((a, b) => (chars(b).length > chars(a).length ? b : a), "");

  /** 光标挂在「最新打出的那一段」后面：词有字就在词后，词空了就在前缀后 */
  const caretAt = word.length > 0 ? "word" : stemShown > 0 ? "stem" : "lead";
  const caret = !reduced && (
    <span className="relative">
      <motion.span
        key={frame.done ? "done" : frame.busy ? "solid" : "blink"}
        animate={
          frame.done
            ? { opacity: 0, transition: { delay: 0.8, duration: DURATION.base } }
            : frame.busy
              ? { opacity: 1 }
              : { opacity: [1, 1, 0, 0] }
        }
        transition={{ duration: 1, repeat: frame.busy ? 0 : Infinity, times: [0, 0.5, 0.5, 1] }}
        className="bg-on-stage absolute top-[0.14em] left-[0.02em] h-[0.8em] w-[0.05em] rounded-full"
      />
    </span>
  );

  return (
    <h1 data-slot="typewriter-title" className={className}>
      <span className="sr-only">{lead + stem + (words[0] ?? "") + end}</span>
      <span aria-hidden="true" className="inline-grid text-left">
        {/* 占位：按最长的词排一遍，撑出固定宽度 */}
        <span className="invisible col-start-1 row-start-1">
          <span className="max-sm:block">{lead}</span>
          <span className="max-sm:block">{stem + widest + end}</span>
        </span>
        <span className="col-start-1 row-start-1">
          <span className="max-sm:block">
            {leadChars.slice(0, leadShown).join("")}
            {caretAt === "lead" && caret}
          </span>
          <span className="max-sm:block">
            {chars(stem).slice(0, stemShown).join("")}
            {caretAt === "stem" && caret}
            {word.join("")}
            {caretAt === "word" && caret}
            {frame.ended && end}
          </span>
        </span>
      </span>
    </h1>
  );
}
