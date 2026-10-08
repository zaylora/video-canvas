import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from "react";
import {
  AnimatePresence,
  motion,
  useAnimate,
  useDragControls,
  useMotionValue,
  useReducedMotion,
} from "motion/react";

import type { AgentModelDto } from "@/api/agent/type";
import { AGENT_GUIDES } from "@/constants/agent";
import type { AgentController } from "@/hooks/use-agent-controller";
import { useIsMobile } from "@/hooks/use-mobile";
import { DURATION, EASE_OUT, SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { DOCK_WIDTH, useAgentSettings } from "@/store/agent-settings";
import type { Chip } from "@/utils/agent/chips";
import { serializeChip } from "@/utils/agent/chips";

import { AgentComposer } from "./agent-composer";
import type { AgentEditorHandle, NodeOption } from "./agent-editor";
import { AgentEmpty } from "./agent-empty";
import { AgentHeader } from "./agent-header";
import { DecisionDock } from "./decision-dock";
import { MessageList } from "./message-list";
import { RunStrip } from "./run-strip";

/** 点引导项：直接发的，或者切到对应模式并预填一句话（拆分镜带上画布里的剧本节点作为 chip） */
const GUIDE_ACTIONS = {
  inspect: { send: "读一下当前画布，告诉我它现在有什么、还缺什么。" },
  storyboard: { fill: (script?: string) => `把${script ? ` ${script} ` : "画布上的剧本"}拆成分镜` },
  story: { fill: () => "我想把下面这个故事改编成剧本和分镜：\n" },
  polish: { fill: () => "优化选中节点的提示词" },
} as const;

/** 停靠后画布至少留这么宽，放不下就临时退回浮窗 */
const MIN_CANVAS_WIDTH = 480;
/** 调宽手柄每按一次方向键改多少 */
const RESIZE_STEP = 16;

/** 停靠宽度的上限：不超过 560，也不超过窗口的一半 */
const clampDockWidth = (width: number) =>
  Math.round(Math.max(DOCK_WIDTH.min, Math.min(DOCK_WIDTH.max, window.innerWidth * 0.5, width)));

/** 窗口宽度，随窗口变化更新 */
function useWindowWidth() {
  const [width, setWidth] = useState(() => window.innerWidth);
  useEffect(() => {
    const onResize = () => setWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return width;
}

/**
 * 画布 Agent 浮窗（设计稿 6.8）。三种形态共用同一棵元素树（切换时输入框里的草稿不丢）：
 * - 浮窗：右下角 400×600 的圆角面板，拖顶栏移动、限制在画布内；
 * - 停靠：画布右侧的全高侧栏，作为和画布并列的一列让出宽度；拖左边缘调宽（拖动时侧栏跟手、松手后画布才让出），双击复位；
 * - 窄屏：底部 Sheet，不能拖动、不能停靠。
 * 停靠后窗口变窄放不下时临时退回浮窗，偏好不变。等你决定时，决定框替换输入框钉在原位。
 */
export function AgentPanel({
  ctl,
  models,
  selectionCount,
  nodes,
  onAttachImages,
  onClose,
}: {
  ctl: AgentController;
  models: AgentModelDto[];
  /** 画布上选中的节点数 */
  selectionCount: number;
  /** 画布上能用 @ 引用的节点 */
  nodes: NodeOption[];
  /** 把图片传到画布，返回新建节点的 chip */
  onAttachImages: (files: File[]) => Promise<Chip[]>;
  onClose: () => void;
}) {
  const mobile = useIsMobile();
  const reduce = useReducedMotion();
  const windowWidth = useWindowWidth();
  const showThinking = useAgentSettings((s) => s.showThinking);
  const wantDock = useAgentSettings((s) => s.docked);
  const dockWidth = useAgentSettings((s) => s.dockWidth);
  const canDock = windowWidth - dockWidth >= MIN_CANVAS_WIDTH;
  const docked = wantDock && canDock && !mobile;

  const [text, setText] = useState("");
  const [useSelection, setUseSelection] = useState(true);
  const [scrolled, setScrolled] = useState(false);
  const editor = useRef<AgentEditorHandle>(null);
  const bounds = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [scope, animate] = useAnimate<HTMLDivElement>();
  const controls = useDragControls();
  const offset = useAgentSettings((s) => s.offset);
  const x = useMotionValue(offset?.dx ?? 0);
  const y = useMotionValue(offset?.dy ?? 0);

  // 窗口变小后记住的位置可能已经在画布外：浮窗出现时检查一次，出界就回到默认位置
  useEffect(() => {
    const p = panel.current?.getBoundingClientRect();
    const b = bounds.current?.getBoundingClientRect();
    if (!p || !b || mobile || docked) return;
    if (
      p.left < b.left - 1 ||
      p.top < b.top - 1 ||
      p.right > b.right + 1 ||
      p.bottom > b.bottom + 1
    ) {
      x.set(0);
      y.set(0);
      useAgentSettings.getState().patch({ offset: null });
    }
  }, [mobile, docked, x, y]);

  // 切换停靠：新形态从右侧（停靠）或入口方向（浮窗）进入；首次挂载由 initial 负责
  const mounted = useRef(false);
  useEffect(() => {
    if (!mounted.current) {
      mounted.current = true;
      return;
    }
    if (!scope.current) return;
    const from = reduce
      ? { opacity: [0, 1] }
      : docked
        ? { opacity: [0, 1], x: [24, 0] }
        : { opacity: [0, 1], y: [8, 0], scale: [0.96, 1] };
    void animate(scope.current, from, { duration: DURATION.base, ease: EASE_OUT });
  }, [docked, reduce, animate, scope]);

  const send = async (message: string, mode = ctl.mode) => {
    if (mode !== ctl.mode) ctl.setMode(mode);
    const ok = await ctl.send(message, useSelection);
    // 发出去了才清空输入框；没发出去（接口报错）保留原文，用户可以改了再发
    if (ok) editor.current?.clear();
    return ok;
  };

  const onGuide = (id: (typeof AGENT_GUIDES)[number]["id"]) => {
    const guide = AGENT_GUIDES.find((g) => g.id === id);
    const action = GUIDE_ACTIONS[id];
    if (!guide) return;
    if ("send" in action) {
      void send(action.send, guide.mode);
      return;
    }
    ctl.setMode(guide.mode);
    const script = nodes.find((n) => n.kind === "script");
    const chip =
      script && id === "storyboard"
        ? serializeChip({ type: "node", id: script.id, name: script.label })
        : undefined;
    // 空会话里编辑器和引导项同时挂着，所以预填后把光标放进去就能接着写
    editor.current?.setText(action.fill(chip));
  };

  /** Esc 停止本轮：焦点在浮窗里、输入框为空、没有在等你决定（弹层在浮窗外，按 Esc 只关弹层） */
  const onKeyDown = (e: ReactKeyboardEvent) => {
    if (e.key !== "Escape" || !ctl.busy || ctl.pending || text.trim()) return;
    if (!scope.current?.contains(e.target as Node)) return;
    e.preventDefault();
    void ctl.stop();
  };

  const empty = ctl.timeline.length === 0 && !ctl.busy;
  const enter = reduce
    ? { opacity: 0 }
    : docked
      ? { opacity: 0, x: 24 }
      : mobile
        ? { opacity: 0, y: 24 }
        : { opacity: 0, y: 8, scale: 0.96 };
  const exit = reduce
    ? { opacity: 0 }
    : docked
      ? { opacity: 0, x: 24 }
      : mobile
        ? { opacity: 0, y: 24 }
        : { opacity: 0, scale: 0.96 };

  const body = (
    <>
      <AgentHeader
        ctl={ctl}
        title={ctl.session?.title ?? "新对话"}
        docked={docked}
        canDock={mobile ? null : canDock}
        scrolled={!empty && scrolled}
        onToggleDock={() => useAgentSettings.getState().patch({ docked: !docked })}
        onClose={onClose}
        onDragStart={mobile || docked ? undefined : (e) => controls.start(e)}
      />
      {empty ? (
        <AgentEmpty onGuide={onGuide} />
      ) : (
        <MessageList
          key={ctl.session?.id ?? "new"}
          ctl={ctl}
          showThinking={showThinking}
          nodes={nodes}
          onScrolled={setScrolled}
        />
      )}
      <AnimatePresence>
        {ctl.strip && <RunStrip key={ctl.strip.runId} strip={ctl.strip} onHide={ctl.hidePlan} />}
      </AnimatePresence>
      {/* 决定框和输入框共用一个容器：互换时容器用 layout 过渡高度，只在互换时触发（打字换行不做动画） */}
      <motion.div
        layout
        layoutDependency={ctl.pending?.id ?? null}
        transition={{ layout: SPRING }}
        style={{ borderRadius: 18 }}
        className="agent-composer mx-3 mb-3 shrink-0 overflow-hidden"
      >
        <AnimatePresence mode="popLayout" initial={false}>
          {ctl.pending && (
            <motion.div
              key={ctl.pending.id}
              layout="position"
              layoutDependency={ctl.pending.id}
              initial={reduce ? { opacity: 0 } : { opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
              transition={{ duration: DURATION.base, ease: EASE_OUT }}
            >
              <DecisionDock approval={ctl.pending} ctl={ctl} />
            </motion.div>
          )}
        </AnimatePresence>
        {/* 等你决定时输入框只是藏起来，不卸载：里面写了一半的话还在 */}
        <div hidden={ctl.pending !== null}>
          <AgentComposer
            text={text}
            onTextChange={setText}
            onSend={() => void send(text)}
            onStop={() => void ctl.stop()}
            busy={ctl.busy}
            sending={ctl.sending}
            mode={ctl.mode}
            onModeChange={ctl.setMode}
            models={models}
            modelKey={ctl.modelKey}
            usage={ctl.usage}
            selectionCount={selectionCount}
            useSelection={useSelection}
            onToggleSelection={() => setUseSelection(false)}
            editorRef={editor}
            nodes={nodes}
            onAttachImages={onAttachImages}
          />
        </div>
      </motion.div>
    </>
  );

  const shell = "@container text-popover-foreground flex flex-col overflow-hidden";

  if (mobile) {
    return (
      <motion.div
        ref={scope}
        role="dialog"
        aria-label="画布 Agent"
        onKeyDown={onKeyDown}
        initial={enter}
        animate={{ opacity: 1, y: 0 }}
        exit={{ ...exit, transition: { duration: DURATION.exit } }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        className={cn(
          shell,
          "agent-panel pointer-events-auto fixed inset-x-0 bottom-0 z-30 h-[82%] rounded-t-3xl backdrop-blur-xl",
        )}
      >
        {body}
      </motion.div>
    );
  }

  return (
    <div
      ref={bounds}
      className={docked ? "relative h-full shrink-0" : "pointer-events-none absolute inset-3 z-20"}
      style={
        docked
          ? ({ width: dockWidth, "--dock-live": `${dockWidth}px` } as CSSProperties)
          : undefined
      }
    >
      {docked && <DockResizer width={dockWidth} wrapper={bounds} />}
      {/* 外层只管拖动（x、y），入场和退场动画在内层，两者不抢同一个动效值 */}
      <motion.div
        ref={panel}
        drag={!docked}
        dragControls={controls}
        dragListener={false}
        dragMomentum={false}
        dragElastic={0}
        dragConstraints={bounds}
        onDragEnd={() =>
          useAgentSettings.getState().patch({ offset: { dx: x.get(), dy: y.get() } })
        }
        style={docked ? { x: 0, y: 0 } : { x, y }}
        className={
          docked
            ? "absolute inset-y-0 right-0 w-(--dock-live)"
            : "pointer-events-auto absolute right-0 bottom-14 h-[min(600px,100%)] w-[400px] max-w-full"
        }
      >
        <motion.div
          ref={scope}
          role="dialog"
          aria-label="画布 Agent"
          onKeyDown={onKeyDown}
          initial={enter}
          animate={{ opacity: 1, x: 0, y: 0, scale: 1 }}
          exit={{ ...exit, transition: { duration: DURATION.exit } }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          style={{ transformOrigin: docked ? "right center" : "bottom right" }}
          className={cn(
            shell,
            "h-full w-full",
            docked ? "agent-docked" : "agent-panel rounded-3xl backdrop-blur-xl",
          )}
        >
          {body}
        </motion.div>
      </motion.div>
    </div>
  );
}

/**
 * 停靠侧栏左边缘的调宽手柄：6px 热区，悬停 / 拖动 / 聚焦时显示 2px 线。
 * 拖动时只改外层的 --dock-live（侧栏跟手、盖在画布上，不触发画布重排），松手才写进设置让画布让出宽度。
 * 双击恢复默认宽度；聚焦后 ←/→ 每次调 16px。
 */
function DockResizer({
  width,
  wrapper,
}: {
  width: number;
  wrapper: RefObject<HTMLDivElement | null>;
}) {
  const [dragging, setDragging] = useState(false);
  const commit = (next: number) =>
    useAgentSettings.getState().patch({ dockWidth: clampDockWidth(next) });
  const live = (e: ReactPointerEvent) => clampDockWidth(window.innerWidth - e.clientX);

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="调整 Agent 宽度"
      aria-valuenow={width}
      aria-valuemin={DOCK_WIDTH.min}
      aria-valuemax={DOCK_WIDTH.max}
      tabIndex={0}
      title="拖动调宽 · 双击恢复"
      data-dragging={dragging || undefined}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        setDragging(true);
      }}
      onPointerMove={(e) => {
        if (dragging) wrapper.current?.style.setProperty("--dock-live", `${live(e)}px`);
      }}
      onPointerUp={(e) => {
        if (!dragging) return;
        setDragging(false);
        commit(live(e));
      }}
      onDoubleClick={() => commit(DOCK_WIDTH.default)}
      onKeyDown={(e) => {
        if (e.key === "ArrowLeft") commit(width + RESIZE_STEP);
        if (e.key === "ArrowRight") commit(width - RESIZE_STEP);
      }}
      className="group/resize absolute inset-y-0 right-[calc(var(--dock-live)-3px)] z-10 w-1.5 cursor-col-resize outline-none"
    >
      <span
        aria-hidden
        className="bg-node-ring/40 absolute inset-y-0 left-0.5 w-0.5 opacity-0 transition-opacity duration-120 group-hover/resize:opacity-100 group-focus-visible/resize:opacity-100 group-data-dragging/resize:opacity-100"
      />
    </div>
  );
}
