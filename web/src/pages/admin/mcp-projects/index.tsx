import { MotionConfig } from "motion/react";

import { MCPProjectsView, useMCPProjects } from "@/components/mcp-projects";

/**
 * 后台「MCP 接入」：管理员代管全部项目，能给任意用户建接入、签发令牌。
 * 用户自己开接入不走这里，走「我的 MCP 接入」（/mcp）。
 */
export default function MCPProjectsPage() {
  const state = useMCPProjects("admin");
  return (
    <MotionConfig reducedMotion="user">
      <MCPProjectsView state={state} mode="admin" />
    </MotionConfig>
  );
}
