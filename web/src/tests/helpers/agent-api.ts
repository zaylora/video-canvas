import { spyOn } from "bun:test";

import * as agentApi from "@/api/agent";

/** 测试用 API 替身：参数和返回值由各用例定义，导出名必须属于真实模块。 */
type AgentApiOverrides = Partial<Record<keyof typeof agentApi, (...args: never[]) => unknown>>;

/**
 * 保留 API 模块的完整导出，只替换当前用例需要的函数；调用返回值可恢复原实现。
 * 整模块 mock 会跨文件保留，导致后加载的控制器找不到未提供的导出。
 * @param overrides 当前用例的 API 实现
 * @returns 恢复所有替身的函数
 */
export function mockAgentApi(overrides: AgentApiOverrides): () => void {
  const spies = Object.entries(overrides).map(([name, implementation]) =>
    spyOn(agentApi, name as keyof typeof agentApi).mockImplementation(implementation as never),
  );
  return () => {
    for (const spy of spies) spy.mockRestore();
  };
}
