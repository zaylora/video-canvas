import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { AnimatePresence } from "motion/react";
import type { ReactNode } from "react";

import { useCanvasEditor } from "@/components/canvas-editor/editor-api";
import {
  useAgentController,
  useAgentModels,
  type AgentController,
} from "@/hooks/use-agent-controller";
import type { AgentModelDto } from "@/api/agent/type";
import type { CanvasNode } from "@/types";
import { isReferableKind, type RefNode } from "@/utils/agent/selection-refs";

import { AgentPanel } from "./agent/agent-panel";

type AgentHost = {
  /** 可用的 Agent 模型；null 是还在加载 */
  models: AgentModelDto[] | null;
  /** 浮窗是否打开 */
  open: boolean;
  toggle: () => void;
  ctl: AgentController;
};

const AgentHostContext = createContext<AgentHost | null>(null);

/** 取画布 Agent 的状态；只能在 AgentHostProvider 里用 */
export function useAgentHost(): AgentHost {
  const host = useContext(AgentHostContext);
  if (!host) throw new Error("useAgentHost 必须在 AgentHostProvider 内部使用");
  return host;
}

/**
 * 画布 Agent 浮窗的全部状态：控制器常驻（收起后运行状态、事件接收照常），⌘/ 开关。
 * 包在 CanvasEditor 外面（要在 ReactFlowProvider 里，取视口用），状态变化只重渲染用到它的外壳组件，
 * 不会牵动整个画布编辑器。
 */
export function AgentHostProvider({
  canvasId,
  children,
}: {
  canvasId: string;
  children: ReactNode;
}) {
  const { getViewport } = useReactFlow();
  const models = useAgentModels();
  const [open, setOpen] = useState(false);
  const ctl = useAgentController({ canvasId, models: models ?? [], getViewport });
  const toggle = useCallback(() => setOpen((current) => !current), []);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "/") {
        event.preventDefault();
        setOpen((current) => !current);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const value = useMemo(() => ({ models, open, toggle, ctl }), [ctl, models, open, toggle]);
  return <AgentHostContext.Provider value={value}>{children}</AgentHostContext.Provider>;
}

/** 与画布并排的 Agent 侧栏（CanvasEditor 的 aside 插槽）；浮窗时 Agent 盖在画布上（设计稿 画布Agent助手设计 6.8） */
export function AgentDock() {
  const { nodes } = useCanvasEditor();
  const { ctl, models, open, toggle } = useAgentHost();
  // 画布选中的节点 id（含组）。nodes 拖动时每帧都是新数组，这里拼成字符串再拆，选中没变时引用不变
  const selectionKey = nodes
    .filter((node) => node.selected)
    .map((node) => node.id)
    .join("\n");
  const selection = useMemo(() => (selectionKey ? selectionKey.split("\n") : []), [selectionKey]);
  /** 输入框里 @ 能引用的节点（组不算） */
  const nodeOptions = useMemo(
    () =>
      nodes
        .filter((node): node is CanvasNode => node.type === "canvas")
        .flatMap((node) => {
          const { kind } = node.data;
          // 导演台 Agent 读不懂，不进 @ 候选；角色卡可以
          return isReferableKind(kind) ? [{ id: node.id, label: node.data.label, kind }] : [];
        }),
    [nodes],
  );
  /** 选中节点的浅色占位解析标题用：组也要能解析（输入框里 @ 的候选不含组） */
  const refOptions = useMemo(
    () =>
      nodes.flatMap((node): RefNode[] => {
        if (node.type === "group") return [{ id: node.id, label: node.data.label, kind: "group" }];
        const { kind } = node.data;
        return isReferableKind(kind) ? [{ id: node.id, label: node.data.label, kind }] : [];
      }),
    [nodes],
  );
  return (
    <AnimatePresence>
      {open && models && models.length > 0 && (
        <AgentPanel
          key="agent-panel"
          ctl={ctl}
          models={models}
          selection={selection}
          nodes={nodeOptions}
          refNodes={refOptions}
          onClose={toggle}
        />
      )}
    </AnimatePresence>
  );
}
