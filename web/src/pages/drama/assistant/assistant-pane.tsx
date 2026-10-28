import { Bot, BookOpenText, Clapperboard, FilePen } from "lucide-react";

import { Skeleton } from "@/components/ui/skeleton";
import { AgentPanel } from "@/pages/canvas/agent/agent-panel";
import type { AgentEmptyCopy } from "@/pages/canvas/agent/agent-empty";

import { useDramaAssistant } from "./use-drama-assistant";

const NO_NODES: never[] = [];

/** 短剧助手的空会话文案：起步建议只填进输入框，不直接发送 */
const EMPTY_COPY: AgentEmptyCopy = {
  title: "让助手帮你改剧本、资产和分镜",
  subtitle: "先读后改；改剧本、改片段、提交任务前都会给出审批卡，批准后才写入",
  starters: [
    { icon: BookOpenText, text: "读一遍大纲，告诉我哪里可以更紧凑" },
    { icon: FilePen, text: "把第 1 集的结尾改得更有悬念" },
    { icon: Clapperboard, text: "检查各集的分镜片段，看提示词有没有遗漏" },
  ],
  hint: null,
};

/**
 * 短剧助手：复用画布 Agent 的会话 / 消息 / 审批 / 输入组件（AgentPanel 的 embedded 形态），
 * 不复制组件；没有画布，所以没有节点引用和选中占位。
 */
export function AssistantPane() {
  const { ctl, models } = useDramaAssistant();
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
        <p className="text-muted-foreground text-xs">请联系管理员发布 Agent 模型后再来。</p>
      </div>
    );
  }
  return (
    <AgentPanel
      variant="embedded"
      label="短剧助手"
      ctl={ctl}
      models={models}
      selection={NO_NODES}
      nodes={NO_NODES}
      refNodes={NO_NODES}
      emptyCopy={EMPTY_COPY}
    />
  );
}
