import { ArrowRight, Film, ScanSearch, Upload, WandSparkles } from "lucide-react";

import { AGENT_GUIDES } from "@/constants/agent";

const ICONS = { scan: ScanSearch, film: Film, upload: Upload, wand: WandSparkles } as const;

/** 键位提示的小方块 */
function Key({ children }: { children: string }) {
  return (
    <kbd className="ring-chrome-border bg-foreground/5 inline-grid h-[18px] min-w-[18px] place-items-center rounded-[5px] px-1 font-sans text-[11px] ring-1">
      {children}
    </kbd>
  );
}

/**
 * 空会话：带静态光晕的品牌光球（规范不允许常驻循环动画）+ 标题 + 副标题 + 4 个引导项 + 键位提示。
 * 内容放不下时从顶部开始滚动（safe center），不会被居中裁掉。
 */
export function AgentEmpty({
  onGuide,
}: {
  onGuide: (id: (typeof AGENT_GUIDES)[number]["id"]) => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-[safe_center] gap-1.5 overflow-y-auto px-5 py-6">
      <div aria-hidden className="relative grid h-24 w-30 shrink-0 place-items-center">
        <div className="absolute inset-0 rounded-full bg-[radial-gradient(closest-side,color-mix(in_oklab,var(--preset)_30%,transparent),transparent)]" />
        <div className="agent-orb relative size-16 rounded-full opacity-90 shadow-[inset_0_1px_1px_color-mix(in_oklab,white_40%,transparent),0_10px_30px_-6px_color-mix(in_oklab,var(--preset)_60%,transparent)]" />
      </div>
      <p className="text-[17px] font-semibold tracking-[-0.01em]">让 Agent 帮你把想法搭成分镜</p>
      <p className="text-muted-foreground mb-3.5 text-center text-[12.5px]">
        读懂画布、搭建节点、整理布局；花积分和删除前都会先问你
      </p>
      <ul className="flex w-full flex-col gap-2">
        {AGENT_GUIDES.map((g) => {
          const Icon = ICONS[g.icon];
          return (
            <li key={g.id}>
              <button
                type="button"
                onClick={() => onGuide(g.id)}
                className="group agent-inset bg-foreground/3 hover:bg-foreground/6 focus-visible:ring-node-ring/60 flex h-14 w-full items-center gap-3 rounded-xl px-3 text-left text-[13.5px] transition-[background-color,box-shadow] duration-120 outline-none hover:shadow-[inset_0_1px_0_var(--sheen),0_0_0_1px_color-mix(in_oklab,var(--preset)_35%,transparent)] focus-visible:ring-2"
              >
                <span className="agent-inset bg-foreground/6 group-hover:text-preset grid size-8 shrink-0 place-items-center rounded-[10px] transition-colors duration-120">
                  <Icon className="size-4 stroke-[1.75]" />
                </span>
                <span className="flex min-w-0 flex-1 flex-col leading-tight">
                  <span>{g.title}</span>
                  <span className="text-muted-foreground truncate text-[11.5px]">{g.hint}</span>
                </span>
                <ArrowRight className="text-muted-foreground size-4 shrink-0 transition-transform duration-120 group-hover:translate-x-0.5" />
              </button>
            </li>
          );
        })}
      </ul>
      <p className="text-muted-foreground mt-3 flex items-center gap-1.5 text-[11.5px]">
        <Key>⌘ /</Key>随时唤起 · <Key>@</Key>引用节点
      </p>
    </div>
  );
}
