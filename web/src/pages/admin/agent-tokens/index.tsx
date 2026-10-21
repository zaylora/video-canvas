import { MotionConfig } from "motion/react";

import type { AgentTokenItem } from "@/api/admin/agent-token/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { useNow } from "@/hooks/use-now";

import { AgentTokensView } from "./agent-tokens-view";
import { useAgentTokens } from "./use-agent-tokens";

/**
 * Agent 分析令牌页：签发、查看、注销给本地 agent-trace 用的只读令牌。
 * 注销前二次确认，注销后从列表里消失；明文只在刚签发时显示一次。
 */
export default function AgentTokensPage() {
  const api = useAgentTokens();
  /** 每分钟刷新一次，到期的令牌不用刷新页面就会变成已过期 */
  const now = useNow(true, 60_000);

  const askRevoke = (item: AgentTokenItem) =>
    void confirm({
      title: `注销「${item.name}」？`,
      destructive: true,
      confirmLabel: "注销",
      description: "注销后立即失效，使用它的 agent-trace 会收到 401，不可恢复。",
      onConfirm: () => api.revoke(item),
    });

  return (
    <MotionConfig reducedMotion="user">
      <AgentTokensView
        state={api.state}
        values={api.values}
        errors={api.errors}
        issuing={api.issuing}
        now={now}
        onChange={api.change}
        onIssue={() => void api.issue()}
        onDismiss={api.dismiss}
        onRevoke={askRevoke}
        onRetry={() => void api.reload()}
      />
    </MotionConfig>
  );
}
