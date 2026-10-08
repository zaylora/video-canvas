/**
 * 浮窗里弹层的统一外观（设计稿 6.8「视觉精修」）：大圆角、chrome 描边、顶部 1px 内高光、柔和投影。
 * 用 Tailwind 可组合的 ring / shadow / inset-shadow，不和 PopoverContent 自带的 ring 打架。
 */
export const AGENT_POP =
  "rounded-2xl ring-chrome-border shadow-xl inset-shadow-[0_1px_0_var(--sheen)]";

/** 浮窗里的图标按钮：hover、按下、弹层打开、聚焦、禁用各态 */
export const AGENT_ICON_BTN =
  "text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-popup-open:bg-foreground/10 data-popup-open:text-foreground focus-visible:ring-node-ring/60 grid size-[30px] shrink-0 place-items-center rounded-lg outline-none transition-colors duration-120 focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-4 [&_svg]:stroke-[1.75]";

/** 输入框底栏的文字按钮（模式、Agent 模型） */
export const AGENT_BAR_BTN =
  "text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-popup-open:bg-chrome-hover data-popup-open:text-foreground focus-visible:ring-node-ring/60 flex h-8 shrink-0 items-center gap-1 rounded-lg px-2 text-xs whitespace-nowrap outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-40 [&_svg]:stroke-[1.75]";
