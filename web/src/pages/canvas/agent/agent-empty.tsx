/** 键位提示的小方块 */
function Key({ children }: { children: string }) {
  return (
    <kbd className="ring-chrome-border bg-foreground/5 inline-grid h-[18px] min-w-[18px] place-items-center rounded-[5px] px-1 font-sans text-[11px] ring-1">
      {children}
    </kbd>
  );
}

/**
 * 空会话（扁平风格）：40px 品牌光球（无光晕、无投影，规范不允许常驻循环动画）+ 标题 + 副标题 + 键位提示，在面板里居中。
 * 居中用内层的 m-auto：放得下就上下左右居中，窗口矮到放不下时自动变成从顶部开始滚动，不会被裁掉；
 * 滚动条常驻可见（agent-scroll）。
 */
export function AgentEmpty() {
  return (
    <div className="agent-scroll flex min-h-0 flex-1 flex-col px-5 py-5">
      <div className="m-auto flex flex-col items-center gap-1">
        <div aria-hidden className="agent-orb mb-1 size-10 shrink-0 rounded-full" />
        <p className="text-[15px] font-semibold tracking-[-0.01em]">让 Agent 帮你把想法搭成分镜</p>
        <p className="text-muted-foreground mb-3 text-center text-xs">
          读懂画布、搭建节点、整理布局；花积分和删除前都会先问你
        </p>
        <p className="text-muted-foreground flex items-center gap-1.5 text-[11.5px]">
          <Key>⌘ /</Key>随时唤起 · <Key>@</Key>引用节点 · <Key>/</Key>插入技能
        </p>
      </div>
    </div>
  );
}
