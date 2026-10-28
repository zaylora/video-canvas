import { createContext, useContext, useEffect, useMemo, useRef, type ReactNode } from "react";

import type { AgentModelDto } from "@/api/agent/type";
import type { Drama } from "@/api/drama/type";
import {
  useAgentController,
  useAgentModels,
  type AgentController,
} from "@/hooks/use-agent-controller";
import { subscribeLiveAgentEvents } from "@/store/agent";
import { dramaRefreshFor } from "@/utils/agent/drama";

type DramaAssistant = {
  /** 可用的 Agent 模型；null 是还在加载 */
  models: AgentModelDto[] | null;
  /** 和画布 Agent 同一个控制器，会话建在短剧画布上、mode 为 drama */
  ctl: AgentController;
};

const DramaAssistantContext = createContext<DramaAssistant | null>(null);

/** 取短剧助手的状态；只能在 DramaAssistantProvider 里用 */
export function useDramaAssistant(): DramaAssistant {
  const value = useContext(DramaAssistantContext);
  if (!value) throw new Error("useDramaAssistant 必须在 DramaAssistantProvider 内部使用");
  return value;
}

/**
 * 短剧助手的常驻状态：复用画布 Agent 的控制器（mode: "drama"，会话建在这部短剧的画布上，不带视口），
 * 放在工作台 / 片段工作区的最外层，切换右栏标签、收起右栏时运行状态和事件接收都不中断。
 * 工具写入成功后（实时到达的 tool.end，回放的历史不算）重新拉短剧详情：
 * 集和大纲没有专门的推送事件；片段和任务靠已有的 drama.segment / drama.job。
 */
export function DramaAssistantProvider({
  drama,
  refresh,
  children,
}: {
  drama: Drama;
  refresh: () => Promise<void>;
  children: ReactNode;
}) {
  const models = useAgentModels();
  const ctl = useAgentController({ canvasId: drama.canvasId, models: models ?? [], mode: "drama" });

  // refresh 每次详情更新都可能换引用，用 ref 让订阅只建一次
  const refreshRef = useRef(refresh);
  useEffect(() => {
    refreshRef.current = refresh;
  }, [refresh]);
  useEffect(
    () =>
      subscribeLiveAgentEvents((event) => {
        if (dramaRefreshFor(event, drama.canvasId)) void refreshRef.current();
      }),
    [drama.canvasId],
  );

  const value = useMemo(() => ({ models, ctl }), [ctl, models]);
  return <DramaAssistantContext.Provider value={value}>{children}</DramaAssistantContext.Provider>;
}
