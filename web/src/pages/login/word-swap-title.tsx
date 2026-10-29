import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";
import { wordIndexAt } from "@/utils/login/word-swap";

/** 标题前半句和轮换的词（句号跟着词走，词变长时空白落在句末，不夹在词和句号中间） */
const LEAD = "从一句话，到一部";
const WORDS = ["片。", "电影。", "短剧。"];

/** 每个词停留的时间 */
const INTERVAL = 2800;

/**
 * 登录页大标题「从一句话，到一部片。」：末尾的词在「片 / 电影 / 短剧」之间轮换。
 * 三个词叠在同一个格子里（宽度取最长的），换词时整行不跳；新词淡入、上移、去模糊，旧词反向退出。
 * 读屏读整句（第一个词），不读轮换中的碎片；减少动态效果时只淡入淡出。
 */
export function WordSwapTitle({ className }: { className?: string }) {
  const [index, setIndex] = useState(0);

  useEffect(() => {
    const start = performance.now();
    const timer = window.setInterval(
      () => setIndex(wordIndexAt(performance.now() - start, WORDS.length, INTERVAL)),
      INTERVAL / 4,
    );
    return () => window.clearInterval(timer);
  }, []);

  return (
    <h1 aria-label={`${LEAD}${WORDS[0]}`} className={className}>
      <span aria-hidden>
        {LEAD}
        <span className="text-beam-bright inline-grid text-left">
          {WORDS.map((word, i) => (
            <span
              key={word}
              className={cn(
                "col-start-1 row-start-1 transition-[opacity,transform,filter] duration-[320ms,420ms,320ms] ease-[ease,cubic-bezier(0.23,1,0.32,1),ease] motion-reduce:transform-none! motion-reduce:filter-none!",
                i !== index && "translate-y-[0.18em] opacity-0 blur-[6px]",
              )}
            >
              {word}
            </span>
          ))}
        </span>
      </span>
    </h1>
  );
}
