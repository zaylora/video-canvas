import { useEffect, useRef, useState } from "react";
import { motion, useDragControls, useMotionValue, useReducedMotion } from "motion/react";

import type { AgentModelDto } from "@/api/agent/type";
import { AGENT_GUIDES } from "@/constants/agent";
import type { AgentController } from "@/hooks/use-agent-controller";
import { useIsMobile } from "@/hooks/use-mobile";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentSettings } from "@/store/agent-settings";

import type { Chip } from "@/utils/agent/chips";
import { serializeChip } from "@/utils/agent/chips";

import { AgentComposer } from "./agent-composer";
import type { AgentEditorHandle, NodeOption } from "./agent-editor";
import { AgentEmpty } from "./agent-empty";
import { AgentHeader } from "./agent-header";
import { MessageList } from "./message-list";
import { RunStrip } from "./run-strip";

/** 点引导项：直接发的，或者切到对应模式并预填一句话（拆分镜带上画布里的剧本节点作为 chip） */
const GUIDE_ACTIONS = {
  inspect: { send: "读一下当前画布，告诉我它现在有什么、还缺什么。" },
  storyboard: { fill: (script?: string) => `把${script ? ` ${script} ` : "画布上的剧本"}拆成分镜` },
  story: { fill: () => "我想把下面这个故事改编成剧本和分镜：\n" },
  polish: { fill: () => "优化选中节点的提示词" },
} as const;

/**
 * 画布 Agent 浮窗：右下角 400×600 的圆角面板，拖顶栏移动、限制在画布内；窄屏变成底部 Sheet 不能拖动。
 * 打开 / 收起从入口按钮（右下）方向缩放淡入淡出；减少动态效果时只淡入淡出。
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
  const showThinking = useAgentSettings((s) => s.showThinking);
  const [text, setText] = useState("");
  const [useSelection, setUseSelection] = useState(true);
  const editor = useRef<AgentEditorHandle>(null);
  const bounds = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const controls = useDragControls();
  const offset = useAgentSettings((s) => s.offset);
  const x = useMotionValue(offset?.dx ?? 0);
  const y = useMotionValue(offset?.dy ?? 0);

  // 窗口变小后记住的位置可能已经在画布外：打开时检查一次，出界就回到默认位置
  useEffect(() => {
    const p = panel.current?.getBoundingClientRect();
    const b = bounds.current?.getBoundingClientRect();
    if (!p || !b || mobile) return;
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
  }, [mobile, x, y]);

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

  const empty = ctl.timeline.length === 0 && !ctl.busy;
  const enter = reduce ? { opacity: 0 } : { opacity: 0, y: 8, scale: 0.96 };

  const body = (
    <>
      <AgentHeader
        ctl={ctl}
        models={models}
        title={ctl.session?.title ?? "新对话"}
        onClose={onClose}
        onDragStart={mobile ? undefined : (e) => controls.start(e)}
      />
      {empty ? (
        <AgentEmpty onGuide={onGuide} />
      ) : (
        <MessageList key={ctl.session?.id ?? "new"} ctl={ctl} showThinking={showThinking} />
      )}
      {ctl.strip && <RunStrip strip={ctl.strip} onHide={ctl.hidePlan} />}
      <AgentComposer
        text={text}
        onTextChange={setText}
        onSend={() => void send(text)}
        onStop={() => void ctl.stop()}
        busy={ctl.busy}
        sending={ctl.sending}
        mode={ctl.mode}
        onModeChange={ctl.setMode}
        selectionCount={selectionCount}
        useSelection={useSelection}
        onToggleSelection={() => setUseSelection(false)}
        editorRef={editor}
        nodes={nodes}
        onAttachImages={onAttachImages}
      />
    </>
  );

  const shell = cn(
    "bg-popover/92 ring-chrome-border text-popover-foreground flex flex-col overflow-hidden shadow-2xl ring-1 backdrop-blur-xl",
  );

  if (mobile) {
    return (
      <motion.div
        role="dialog"
        aria-label="画布 Agent"
        initial={reduce ? { opacity: 0 } : { opacity: 0, y: 24 }}
        animate={{ opacity: 1, y: 0 }}
        exit={
          reduce ? { opacity: 0 } : { opacity: 0, y: 24, transition: { duration: DURATION.exit } }
        }
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        className={cn(
          shell,
          "pointer-events-auto fixed inset-x-0 bottom-0 z-30 h-[82%] rounded-t-3xl",
        )}
      >
        {body}
      </motion.div>
    );
  }

  return (
    <div ref={bounds} className="pointer-events-none absolute inset-3 z-20">
      {/* 外层只管拖动（x、y），入场和退场动画在内层，两者不抢同一个动效值 */}
      <motion.div
        ref={panel}
        drag
        dragControls={controls}
        dragListener={false}
        dragMomentum={false}
        dragElastic={0}
        dragConstraints={bounds}
        onDragEnd={() =>
          useAgentSettings.getState().patch({ offset: { dx: x.get(), dy: y.get() } })
        }
        style={{ x, y }}
        className="pointer-events-auto absolute right-0 bottom-14 h-[min(600px,100%)] w-[400px] max-w-full"
      >
        <motion.div
          role="dialog"
          aria-label="画布 Agent"
          initial={enter}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={
            reduce
              ? { opacity: 0 }
              : { opacity: 0, scale: 0.96, transition: { duration: DURATION.exit } }
          }
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          style={{ transformOrigin: "bottom right" }}
          className={cn(shell, "h-full w-full rounded-3xl")}
        >
          {body}
        </motion.div>
      </motion.div>
    </div>
  );
}
