import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
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
import { ASSISTANT_NAME } from "@/constants/agent";
import type { AgentController } from "@/hooks/use-agent-controller";
import { useIsMobile } from "@/hooks/use-mobile";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { DOCK_WIDTH, useAgentSettings } from "@/store/agent-settings";
import { usePetStore } from "@/store/pet";
import { PET_MARGIN, panelSize, placePanel } from "@/utils/pet/placement";
import { useAgentAttachments } from "@/hooks/use-agent-attachments";
import { toAttachmentsPayload } from "@/utils/agent/attachments";

import { AgentComposer, type ComposerFeatures } from "./agent-composer";
import type { ContextRef } from "./context-chips";
import type { RefNode } from "@/utils/agent/selection-refs";

import type { AgentEditorHandle, NodeOption } from "./agent-editor";
import { AgentEmpty, type AgentEmptyCopy } from "./agent-empty";
import { AgentLoading } from "./agent-loading";
import { AgentHeader } from "./agent-header";
import { WatchStrip } from "./watch-strip";
import { MessageList } from "./message-list";
import { RunStrip } from "./run-strip";
import { SteerTray } from "./steer-tray";

/** 停靠后画布至少留这么宽，放不下就临时退回浮窗 */
const MIN_CANVAS_WIDTH = 480;
/** 调宽手柄每按一次方向键改多少 */
const RESIZE_STEP = 16;
/** 停靠侧栏打开 / 关闭时外层宽度的过渡：和后台助手抽屉同样的 200ms ease-out */
const DOCK_REVEAL = { duration: 0.2, ease: "easeOut" } as const;

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
 * 停靠后窗口变窄放不下时临时退回浮窗，偏好不变。等你决定的审批和提问在消息流里是交互卡片，输入框始终可用。
 */
export function AgentPanel({
  ctl,
  models,
  selection,
  nodes,
  refNodes,
  onClose,
  variant = "floating",
  label = ASSISTANT_NAME,
  emptyCopy,
  features,
  placeholder,
  contextRefs,
  onRemoveContextRef,
  closeShortcut,
  mascot,
}: {
  ctl: AgentController;
  models: AgentModelDto[];
  /** 画布上选中的节点 id（含组）：输入框里先显示浅色占位，激活输入框后变成引用 */
  selection: readonly string[];
  /** 画布上能用 @ 引用的节点 */
  nodes: NodeOption[];
  /** 解析选中节点标题用：含组（nodes 是输入框 @ 的候选，不含组） */
  refNodes: RefNode[];
  /** 收起；嵌在页面里（embedded）时不传，顶栏不显示收起按钮 */
  onClose?: () => void;
  /**
   * floating 画布里的浮窗 / 停靠 / 窄屏 Sheet（默认）；
   * embedded 填满父容器的一栏（短剧工作台右栏），不能拖动、不能停靠，没有进出场动画
   */
  variant?: "floating" | "embedded";
  /** 无障碍名称 */
  label?: string;
  /** 空会话的文案，默认是画布 Agent 的 */
  emptyCopy?: AgentEmptyCopy;
  /** 输入区的画布能力开关（附件、@ 节点、插入模型、技能），缺省全开；后台助手全部关掉 */
  features?: ComposerFeatures;
  /** 输入框占位文字，缺省是画布 Agent 的 */
  placeholder?: string;
  /** 页面上下文引用（后台助手）：显示在输入框里，可移除；缺省没有 */
  contextRefs?: readonly ContextRef[];
  onRemoveContextRef?: (id: string) => void;
  /** 「收起」按钮提示里的快捷键，缺省是画布的 ⌘/ */
  closeShortcut?: string;
  /** 趴在输入框上沿探头的形象（画布里桌宠活跃时是小镜）：靠右，只露出上半身，下半身被框沿挡住 */
  mascot?: ReactNode;
}) {
  const mobile = useIsMobile();
  const reduce = useReducedMotion();
  const windowWidth = useWindowWidth();
  const showThinking = useAgentSettings((s) => s.showThinking);
  const wantDock = useAgentSettings((s) => s.docked);
  const dockWidth = useAgentSettings((s) => s.dockWidth);
  const canDock = windowWidth - dockWidth >= MIN_CANVAS_WIDTH;
  const embedded = variant === "embedded";
  const docked = wantDock && canDock && !mobile && !embedded;

  const [text, setText] = useState("");
  const attachments = useAgentAttachments();
  /** 当前模型能不能看图；没选模型时按会话默认（清单第一个）算 */
  const vision = (models.find((m) => m.key === ctl.modelKey) ?? models[0])?.vision ?? true;
  const [scrolled, setScrolled] = useState(false);
  const editor = useRef<AgentEditorHandle>(null);
  const bounds = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [scope, animate] = useAnimate<HTMLDivElement>();
  const controls = useDragControls();
  const offset = useAgentSettings((s) => s.offset);
  // 桌宠活跃时浮窗跟着桌宠弹出：位置由桌宠的位置算出，拖过的位置只在本次打开期间有效，不读写 offset
  const petPlacement = usePetStore((s) => (s.active ? s.placement : null));
  const petMode = petPlacement !== null && !mobile && !docked && !embedded;
  const x = useMotionValue(petMode ? 0 : (offset?.dx ?? 0));
  const y = useMotionValue(petMode ? 0 : (offset?.dy ?? 0));
  const petPanel = petMode
    ? placePanel(petPlacement.rect, petPlacement.area, panelSize(petPlacement.area))
    : null;
  const petPanelSize = petMode ? panelSize(petPlacement.area) : null;
  /** 停靠侧栏的打开过渡已经走完：之后宽度变化（调宽、切换停靠）立刻到位 */
  const [entered, setEntered] = useState(() => !docked || !!reduce);
  /** 外层宽度正在过渡：这期间裁掉伸出外层的面板 */
  const [revealing, setRevealing] = useState(false);

  // 窗口变小后记住的位置可能已经在画布外：浮窗出现时检查一次，出界就回到默认位置
  useEffect(() => {
    const p = panel.current?.getBoundingClientRect();
    const b = bounds.current?.getBoundingClientRect();
    if (!p || !b || mobile || docked || petMode) return;
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
  }, [mobile, docked, petMode, x, y]);

  // 桌宠挪了位置（或窗口变化）：浮窗回到桌宠旁边；退出桌宠模式时恢复记住的位置
  useEffect(() => {
    if (petMode) {
      x.set(0);
      y.set(0);
    } else {
      const saved = useAgentSettings.getState().offset;
      x.set(saved?.dx ?? 0);
      y.set(saved?.dy ?? 0);
    }
  }, [petMode, petPlacement, x, y]);

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

  const send = async (message: string) => {
    const ok = await ctl.send(message, toAttachmentsPayload(attachments.items));
    // 发出去了才清空输入框和托盘；没发出去（接口报错）保留原文和图片，用户可以改了再发
    if (ok) {
      editor.current?.clear();
      attachments.clear();
    }
    return ok;
  };

  /** Esc 停止本轮：焦点在浮窗里、输入框为空、没有在等你决定（弹层在浮窗外，按 Esc 只关弹层） */
  const onKeyDown = (e: ReactKeyboardEvent) => {
    if (e.key !== "Escape" || !ctl.busy || ctl.pending || text.trim()) return;
    if (!scope.current?.contains(e.target as Node)) return;
    e.preventDefault();
    void ctl.stop();
  };

  const loading = ctl.opening || ctl.openFailed;
  const empty = ctl.timeline.length === 0 && !ctl.busy && !loading;
  /** 还没被 Agent 读到的插话，排在输入框顶部 */
  const steerQueue = ctl.timeline.flatMap((item) =>
    item.type === "user" && item.queued === "waiting"
      ? [{ key: item.key, text: item.text, images: item.attachments.length }]
      : [],
  );
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
        canDock={mobile || embedded ? null : canDock}
        scrolled={!empty && !loading && scrolled}
        onToggleDock={() => useAgentSettings.getState().patch({ docked: !docked })}
        onClose={onClose}
        closeShortcut={closeShortcut}
        onDragStart={mobile || docked || embedded ? undefined : (e) => controls.start(e)}
      />
      {/* 后台等待生成的监听条：头部下方，审批和输入时都看得到；没有进行中的批时不出现 */}
      {ctl.watch && <WatchStrip summary={ctl.watch} onCancel={() => void ctl.cancelWatch()} />}
      {loading ? (
        <AgentLoading failed={ctl.openFailed} onRetry={ctl.reload} />
      ) : empty ? (
        <AgentEmpty
          copy={emptyCopy}
          onStarter={(t) => {
            editor.current?.setText(t);
            editor.current?.focus();
          }}
        />
      ) : (
        <MessageList
          key={ctl.session?.id ?? "new"}
          ctl={ctl}
          showThinking={showThinking}
          nodes={nodes}
          refNodes={refNodes}
          onScrolled={setScrolled}
        />
      )}
      {/* 审批和提问是消息流里的交互卡片，输入框始终可用：等回答时发消息，视为新消息，这张卡被跳过 */}
      <div className={cn("relative mx-3 mb-3 shrink-0", mascot && "mt-9")}>
        {/* 探头的形象：消息列表底部让出它露出来的高度（mt-9），不盖住最后几行；裁掉框沿以下的部分 */}
        {mascot && (
          <div
            aria-hidden
            className="pointer-events-none absolute right-5 bottom-full h-11 [clip-path:inset(-48px_-24px_0_-24px)]"
          >
            {mascot}
          </div>
        )}
        <div className="agent-composer overflow-hidden" style={{ borderRadius: 18 }}>
          <div>
            {/* 计划条并进输入框顶部（设计稿 6.12），不再悬在输入框上方 */}
            <AnimatePresence initial={false}>
              {ctl.strip && (
                <RunStrip key={ctl.strip.runId} strip={ctl.strip} onHide={ctl.hidePlan} />
              )}
            </AnimatePresence>
            <SteerTray items={steerQueue} />
            <AgentComposer
              text={text}
              onTextChange={setText}
              onSend={() => void send(text)}
              onStop={() => void ctl.stop()}
              busy={ctl.busy}
              sending={ctl.sending}
              models={models}
              modelKey={ctl.modelKey}
              context={ctl.context}
              selection={selection}
              refNodes={refNodes}
              editorRef={editor}
              nodes={nodes}
              attachments={attachments}
              vision={vision}
              features={features}
              placeholder={placeholder}
              contextRefs={contextRefs}
              onRemoveContextRef={onRemoveContextRef}
            />
          </div>
        </div>
      </div>
    </>
  );

  const shell = "@container text-popover-foreground flex flex-col overflow-hidden";

  if (embedded) {
    return (
      <div
        ref={scope}
        role="region"
        aria-label={label}
        onKeyDown={onKeyDown}
        className={cn(shell, "h-full min-h-0 w-full")}
      >
        {body}
      </div>
    );
  }

  if (mobile) {
    return (
      <motion.div
        ref={scope}
        role="dialog"
        aria-label={label}
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

  // 停靠侧栏的开合和后台助手抽屉一样：外层宽度从 0 展开到侧栏宽（推挤画布），面板本身不淡入、不偏移；
  // 只有打开和关闭时过渡，拖动调宽、切换停靠时宽度立刻到位
  const reveal = docked && !reduce;
  return (
    <motion.div
      ref={bounds}
      initial={reveal ? { width: 0 } : false}
      animate={{ width: docked ? dockWidth : "auto" }}
      exit={reveal ? { width: 0, transition: DOCK_REVEAL } : undefined}
      transition={{ width: entered ? { duration: 0 } : DOCK_REVEAL }}
      onAnimationStart={() => setRevealing(true)}
      onAnimationComplete={() => {
        setRevealing(false);
        setEntered(true);
      }}
      className={cn(
        docked ? "relative h-full shrink-0" : "pointer-events-none absolute inset-3 z-20",
        docked && revealing && "overflow-hidden",
      )}
      style={
        docked
          ? ({ "--dock-base": `${dockWidth}px`, "--dock-live": `${dockWidth}px` } as CSSProperties)
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
        onDragEnd={() => {
          if (petMode) return;
          useAgentSettings.getState().patch({ offset: { dx: x.get(), dy: y.get() } });
        }}
        style={
          docked
            ? { x: 0, y: 0 }
            : petPanel && petPanelSize
              ? {
                  x,
                  y,
                  left: petPanel.x - PET_MARGIN,
                  top: petPanel.y - PET_MARGIN,
                  width: petPanelSize.width,
                  height: petPanelSize.height,
                }
              : { x, y }
        }
        className={
          docked
            ? // 左边贴住外层左沿：开合时外层变宽，面板跟着左沿滑进来（右侧被裁掉）；
              // 拖动调宽时 --dock-live 和 --dock-base 拉开，面板向左伸出、盖在画布上，右沿不动
              "absolute inset-y-0 left-[calc(var(--dock-base)-var(--dock-live))] w-(--dock-live)"
            : petMode
              ? "pointer-events-auto absolute max-w-full"
              : "pointer-events-auto absolute right-0 bottom-14 h-[min(600px,100%)] w-[400px] max-w-full"
        }
      >
        <motion.div
          ref={scope}
          role="dialog"
          aria-label={label}
          onKeyDown={onKeyDown}
          initial={reveal ? false : enter}
          animate={{ opacity: 1, x: 0, y: 0, scale: 1 }}
          exit={reveal ? undefined : { ...exit, transition: { duration: DURATION.exit } }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          style={{
            transformOrigin: docked ? "right center" : (petPanel?.origin ?? "bottom right"),
          }}
          className={cn(
            shell,
            "h-full w-full",
            docked ? "agent-docked" : "agent-panel rounded-3xl backdrop-blur-xl",
          )}
        >
          {body}
        </motion.div>
      </motion.div>
    </motion.div>
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
