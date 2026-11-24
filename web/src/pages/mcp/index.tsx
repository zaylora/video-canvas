import { MotionConfig } from "motion/react";

import { MCPProjectsView, useMCPProjects } from "@/components/mcp-projects";

/**
 * 我的 MCP 接入：任何登录用户都能给自己开一个接入项目并签发令牌，
 * 让外部 AI 工具（桌面客户端、编辑器插件等）读写自己名下的画布，不必找管理员。
 */
export default function MyMCPPage() {
  const state = useMCPProjects("self");
  return (
    <MotionConfig reducedMotion="user">
      <MCPProjectsView state={state} mode="self" />
    </MotionConfig>
  );
}
