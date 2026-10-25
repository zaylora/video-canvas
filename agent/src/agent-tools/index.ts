import type { Bridge } from "../bridge-client.ts";
import { buildTools } from "../tools.ts";
import type { AgentToolSpec, CodemodeLimits, Log, RunInput, ToolCtx } from "../types.ts";
import { codemodeTool } from "./codemode.ts";

/**
 * Agent 自己的工具注册表：纯计算、纯编排、不碰外部的工具放这里，Go 不需要知道它们的存在（执行时向 Go 报备除外）。
 * 要密钥、联网、花钱、碰用户数据或画布的工具不放这里，放 Go 的注册表。
 *
 * 每一项：{ name, label, verb, applies(ctx) → 这次运行要不要启用, create(ctx) → pi 工具 }。
 * ctx 是 { bridge, decls（Go 下发的工具声明）, vision, onTerminate, log, limits }。
 */
export const AGENT_TOOLS: AgentToolSpec[] = [codemodeTool];

/**
 * 这次运行交给模型的全部工具：Go 下发的在前（顺序不变，提示词缓存要稳定），Agent 自己的在后。
 * 停用清单（启动参数 disabled_tools）里的 Agent 工具不启用；和 Go 的工具重名时 Go 的优先。
 */
export function buildAllTools({ bridge, input, onTerminate = () => {}, log = () => {}, limits }: {
  bridge: Bridge;
  input: RunInput;
  onTerminate?: () => void;
  log?: Log;
  limits?: Partial<CodemodeLimits>;
}) {
  const goTools = buildTools(bridge, input.tools, onTerminate);
  const taken = new Set(goTools.map((t) => t.name));
  const disabled = new Set(input.disabled_tools ?? []);
  const ctx: ToolCtx = { bridge, decls: input.tools ?? [], vision: !!input.model?.vision, onTerminate, log, limits };
  const ours = [];
  for (const t of AGENT_TOOLS) {
    if (disabled.has(t.name) || !t.applies(ctx)) continue;
    if (taken.has(t.name)) {
      log("Agent 工具和 Go 下发的工具重名，用 Go 的", { name: t.name });
      continue;
    }
    ours.push(t.create(ctx));
  }
  return [...goTools, ...ours];
}
