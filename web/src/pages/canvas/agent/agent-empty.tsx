import { ArrowRight, Film, ScanSearch, Upload, WandSparkles } from "lucide-react";

import { AGENT_GUIDES } from "@/constants/agent";

const ICONS = { scan: ScanSearch, film: Film, upload: Upload, wand: WandSparkles } as const;

/** 空会话：静态渐变光球（规范不允许常驻循环动画）+ 一句话 + 4 个引导项 */
export function AgentEmpty({
  onGuide,
}: {
  onGuide: (id: (typeof AGENT_GUIDES)[number]["id"]) => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-5 overflow-y-auto px-5 py-6">
      <div
        aria-hidden
        className="size-16 rounded-full opacity-90 shadow-lg"
        style={{
          background:
            "radial-gradient(circle at 32% 28%, color-mix(in oklab, var(--status-running) 85%, white), transparent 55%), radial-gradient(circle at 70% 75%, var(--preset), color-mix(in oklab, var(--status-running) 55%, black) 85%)",
        }}
      />
      <p className="text-base font-semibold">让 Agent 帮你把想法搭成分镜</p>
      <ul className="flex w-full flex-col gap-2">
        {AGENT_GUIDES.map((g) => {
          const Icon = ICONS[g.icon];
          return (
            <li key={g.id}>
              <button
                type="button"
                onClick={() => onGuide(g.id)}
                className="group bg-foreground/5 hover:bg-foreground/9 ring-chrome-border focus-visible:ring-node-ring/60 flex h-[46px] w-full items-center gap-3 rounded-xl px-3.5 text-left text-[13.5px] ring-1 transition-colors duration-120 outline-none focus-visible:ring-2"
              >
                <Icon className="text-muted-foreground group-hover:text-foreground size-4 shrink-0" />
                <span className="flex-1">{g.title}</span>
                <ArrowRight className="text-muted-foreground size-4 shrink-0 transition-transform duration-120 group-hover:translate-x-0.5" />
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
