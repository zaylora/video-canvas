/**
 * codemode：模型写一小段 JavaScript，一次调用多个工具；中间结果只在脚本里流转，不进模型的上下文。
 * 用 Pi Durable 自带的实现（脚本跑在 QuickJS 虚拟机里，没有网络、文件、进程、定时器）：它的脚本在崩溃后能接着跑——
 * 重启后从记录的启动数据重新开始，已经完成的工具调用按各自的重放策略取回结果，不会再执行一遍。
 *
 * 脚本里能调哪些工具由工具扩展的 callers 决定：Go 标为「可被脚本调用」的才有；
 * 会创建审批并终止本轮的、会返回图片的工具只能由模型直接调用，脚本没法在「等审批」处停下。
 * 脚本里每次调用仍然走平台（幂等键是各自的 Pi 任务 id），事件带上外层调用的 id，整个脚本只算一步：
 * pi 自带的这个工具不经过工具扩展，所以这里用同一个 trackedCall 包一层，在时间线里留一步「运行脚本」。
 */
import { CodemodeSandbox } from "@earendil-works/pi-codemode";
import { defineExtension, type Extension, type ToolExecutionApi, type ToolRegistration } from "@earendil-works/pi-durable";
import { CODEMODE_TOOL_NAME, createCodemodeExtension, type CodemodeToolDetails } from "@earendil-works/pi-durable/codemode";
import type { Context } from "@earendil-works/chord";
import { clip, endEvent, trackedCall, type ToolHost } from "./durable-tools.ts";

export const LABEL = "运行脚本";
export const VERB = "脚本";

/** 单次脚本的总时限（含工具调用）和虚拟机内存上限 */
export const CODEMODE_LIMITS = { timeoutMs: 60_000, memoryLimitBytes: 128 << 20 };

const oneLine = (s: unknown) => String(s ?? "").replace(/\s+/g, " ").trim();

export function createCodemode(host: ToolHost, limits: Partial<typeof CODEMODE_LIMITS> = {}): Extension {
  const { timeoutMs, memoryLimitBytes } = { ...CODEMODE_LIMITS, ...limits };
  const ext = createCodemodeExtension({ sandbox: new CodemodeSandbox({ timeoutMs, memoryLimitBytes }), models: false });
  const info = { name: CODEMODE_TOOL_NAME, label: LABEL, verb: VERB };
  return defineExtension({
    ...ext,
    tools: (ext.tools ?? []).map((t): ToolRegistration => {
      if (t.name !== CODEMODE_TOOL_NAME) return t;
      return {
        ...t,
        execute: (args: never, api: ToolExecutionApi, context: Context) =>
          trackedCall(host, info, args, api, context, async () => {
            const res = await t.execute(args, api, context);
            const output = (res.output ?? []) as Array<{ type: string; text?: string }>;
            const message = output.filter((o) => o.type === "text").map((o) => o.text).join(" ");
            const calls = ((res.details as CodemodeToolDetails | undefined)?.calls ?? []).length;
            const summary = res.isError ? `脚本失败：${clip(oneLine(message), 80)}` : `脚本调用了 ${calls} 次工具`;
            return { result: res as never, end: endEvent({ id: api.callId, ...info }, !!res.isError, summary) };
          }),
      };
    }),
  });
}
