import type { ReactNode } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import { Bot, History, ListChecks, Settings2 } from "lucide-react";

import { MOD } from "@/components/canvas-editor/keys";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminPageContext } from "@/hooks/use-admin-page-context";
import { useAgentController, useAgentModels } from "@/hooks/use-agent-controller";
import { AgentPanel } from "@/pages/canvas/agent/agent-panel";
import type { AgentEmptyCopy } from "@/pages/canvas/agent/agent-empty";
import { useAdminStore } from "@/store/admin";
import { ASSISTANT_NAME } from "@/constants/agent";
import { ADMIN_SESSION_SCOPE } from "@/store/agent";

const NO_NODES: never[] = [];

/** 后台助手没有画布：附件、@ 节点、插入模型、技能都不要 */
const FEATURES = { attachments: false, mentions: false, models: false, skills: false };

/** 空会话文案：只有标题和起步建议（点了只填进输入框），不放说明文字和键位提示 */
const EMPTY_COPY: AgentEmptyCopy = {
  title: "让助手帮你调整后台配置",
  subtitle: null,
  starters: [
    { icon: Settings2, text: "查看当前页面的配置和状态" },
    { icon: History, text: "查询最近失败的任务和运行记录" },
    { icon: ListChecks, text: "查看最近的操作审计" },
  ],
  hint: null,
};

/**
 * 后台助手的空画布作用域：复用的画布 Agent 组件（工具行、节点引用、改动摘要、等待条）会取 ReactFlow 的 hook，
 * 后台没有画布，没有这层会整页报「not used ReactFlowProvider」。后台不涉及节点，定位找不到节点，相关按钮是禁用的
 * @param children 后台助手面板
 */
export function AdminCanvasScope({ children }: { children: ReactNode }) {
  return <ReactFlowProvider>{children}</ReactFlowProvider>;
}

/**
 * 后台助手面板：复用画布 Agent 的面板（AgentPanel 的 embedded 形态）和控制器（mode: "admin"）。
 * 第一次展开抽屉时才挂载，所以没打开过助手的管理员不会多发任何请求。
 * 页面上下文（当前页面、选中的渠道 / 模型 / 插件）作为输入框里的引用 chip，发送时带上。
 */
export function AdminAssistantPanel() {
  const models = useAgentModels();
  const page = useAdminPageContext();
  const setOpen = useAdminStore((s) => s.setAssistantOpen);
  const ctl = useAgentController({
    canvasId: ADMIN_SESSION_SCOPE,
    models: models ?? [],
    mode: "admin",
    pageContext: page.context,
  });
  if (models === null) {
    return (
      <div className="grid gap-3 p-4" aria-busy>
        <Skeleton className="h-6 w-32 motion-reduce:animate-none" />
        <Skeleton className="h-24 w-full motion-reduce:animate-none" />
      </div>
    );
  }
  if (models.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 px-6 py-16 text-center">
        <span className="bg-muted grid size-10 place-items-center rounded-xl">
          <Bot className="size-5" />
        </span>
        <div className="text-sm font-medium">暂时没有可用的 Agent 模型</div>
        <p className="text-muted-foreground text-xs">请先在模型渠道发布 Agent 模型</p>
      </div>
    );
  }
  return (
    <AdminCanvasScope>
      <AgentPanel
        variant="embedded"
        label={ASSISTANT_NAME}
        ctl={ctl}
        models={models}
        selection={NO_NODES}
        nodes={NO_NODES}
        refNodes={NO_NODES}
        emptyCopy={EMPTY_COPY}
        features={FEATURES}
        placeholder="描述要调整的配置"
        contextRefs={page.chips}
        onRemoveContextRef={page.remove}
        onClose={() => setOpen(false)}
        closeShortcut={MOD === "⌘" ? "⌘J" : "Ctrl J"}
      />
    </AdminCanvasScope>
  );
}
