import { motion } from "motion/react";
import { ArrowUpRight, Clapperboard, LayoutGrid, Users, type LucideIcon } from "lucide-react";

import { TAP_CARD } from "@/lib/motion";

/** 键位提示的小方块 */
function Key({ children }: { children: string }) {
  return (
    <kbd className="ring-chrome-border bg-foreground/5 inline-grid h-[18px] min-w-[18px] place-items-center rounded-[5px] px-1 font-sans text-[11px] ring-1">
      {children}
    </kbd>
  );
}

/** 起步建议（设计稿 6.12）：点了只填进输入框，不直接发送，用户可以先改 */
const STARTERS: ReadonlyArray<{ icon: LucideIcon; text: string }> = [
  { icon: Clapperboard, text: "把选中的剧本拆成分镜" },
  { icon: Users, text: "整理人物，做一份资产清单" },
  { icon: LayoutGrid, text: "把画布按场景整理整齐" },
];

/**
 * 空会话（扁平风格）：40px 品牌光球（无光晕、无投影，规范不允许常驻循环动画）+ 标题 + 副标题 + 起步建议 + 键位提示，在面板里居中。
 * 居中用内层的 m-auto：放得下就上下左右居中，窗口矮到放不下时自动变成从顶部开始滚动，不会被裁掉；
 * 滚动条常驻可见（agent-scroll）。
 */
export function AgentEmpty({ onStarter }: { onStarter: (text: string) => void }) {
  return (
    <div className="agent-scroll flex min-h-0 flex-1 flex-col px-5 py-5">
      <div className="m-auto flex w-full flex-col items-center gap-1">
        <div aria-hidden className="agent-orb mb-1 size-10 shrink-0 rounded-full" />
        <p className="text-[15px] font-semibold tracking-[-0.01em]">让 Agent 帮你把想法搭成分镜</p>
        <p className="text-muted-foreground mb-4 text-center text-xs">
          读懂画布、搭建节点、整理布局；花积分和删除前都会先问你
        </p>
        <div className="mb-4 flex w-full flex-col gap-1.5">
          {STARTERS.map(({ icon: Icon, text }) => (
            <motion.button
              key={text}
              type="button"
              whileTap={TAP_CARD}
              onClick={() => onStarter(text)}
              className="group/starter ring-foreground/10 hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex h-10 items-center gap-2.5 rounded-xl px-3 text-left text-[13px] ring-1 outline-none transition-colors duration-120 focus-visible:ring-2"
            >
              <Icon className="text-muted-foreground size-3.5 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{text}</span>
              <ArrowUpRight className="text-muted-foreground size-3.5 shrink-0 -translate-x-1 opacity-0 transition-[opacity,transform] duration-120 group-hover/starter:translate-x-0 group-hover/starter:opacity-100 group-focus-visible/starter:translate-x-0 group-focus-visible/starter:opacity-100" />
            </motion.button>
          ))}
        </div>
        <p className="text-muted-foreground flex items-center gap-1.5 text-[11.5px]">
          <Key>⌘ /</Key>随时唤起 · <Key>@</Key>引用节点 · <Key>/</Key>插入技能
        </p>
      </div>
    </div>
  );
}
