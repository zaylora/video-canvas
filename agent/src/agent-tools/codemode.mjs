import { CodemodeSandbox, renderDeclarations } from "@earendil-works/pi-codemode";
import { wrapScalarArrays } from "../tools.mjs";

/**
 * codemode：模型写一小段 JavaScript，一次调用多个工具；中间结果只在脚本里流转，不进模型的上下文。
 * 脚本跑在 QuickJS 虚拟机里（pi-codemode），唯一能做的事就是调用注入的工具：没有网络、文件、进程、定时器。
 *
 * 每次脚本里的工具调用仍然走桥的 /tool：Go 照常校验模式权限、记 tool.start / tool.end，只是带上父调用 id，
 * 不再各自计入步数（整个脚本算 1 步，由开始时的报备计入）。注入的只有 Go 标为「可被脚本调用」的工具：
 * 会创建审批并终止本轮的、会返回图片的工具不在其中。
 */

export const NAME = "codemode";
export const LABEL = "运行脚本";
export const VERB = "脚本";

/** 默认限制：单次脚本超时、脚本里最多调几次工具、虚拟机内存、返回给模型的文字上限。Go 对一个运行里的嵌套调用总数另有上限 */
export const DEFAULT_LIMITS = { timeoutMs: 60_000, maxNestedCalls: 100, memoryLimitBytes: 128 << 20, maxResultChars: 8000 };

const scriptable = (ctx) => (ctx.decls ?? []).filter((d) => d.scriptable && (d.executor ?? "server") === "server");

export const codemodeTool = {
  name: NAME,
  label: LABEL,
  verb: VERB,
  applies: (ctx) => scriptable(ctx).length > 0,
  create: createCodemode,
};

function createCodemode(ctx) {
  const limits = { ...DEFAULT_LIMITS, ...ctx.limits };
  const decls = scriptable(ctx);
  const declarations = renderDeclarations({ tools: decls.map((d) => ({ name: d.name, description: d.description, inputSchema: d.parameters })) });
  return {
    name: NAME,
    label: LABEL,
    description: describe(declarations, limits),
    parameters: {
      type: "object",
      required: ["code"],
      properties: { code: { type: "string", description: "JavaScript 脚本：异步函数的函数体，可以 await，用 return 返回结果" } },
    },
    execute: (toolCallId, params, signal) => run(ctx, decls, limits, toolCallId, params, signal),
  };
}

function describe(declarations, limits) {
  return [
    "写一段 JavaScript 脚本，在一次调用里连续调用多个工具，中间结果不进你的上下文：批量读取、批量修改、对几十个节点循环处理时用它，比一个个调用工具省得多。",
    "脚本是异步函数的函数体：可以 await，可以 return。用 tools.工具名({参数}) 调用下面列出的工具，参数是 JSON；工具返回的是文字（需要时自己 JSON.parse）。用 text(...) 写进输出，return 的值也会返回给你：只返回你需要的结论，不要把工具的原始结果整个打印出来。",
    `限制：脚本里没有网络、文件、进程和定时器；一次最多运行 ${limits.timeoutMs / 1000} 秒、最多调用工具 ${limits.maxNestedCalls} 次。需要用户确认的操作（删除、生成、提问）和看图不能在脚本里做，要单独调用对应的工具。`,
    "脚本里每次工具调用各自独立生效：脚本中途出错时，出错之前已完成的调用已经改了画布，我会列出来，不要重复执行。",
    "脚本里可以调用的工具：",
    "```ts",
    declarations,
    "```",
  ].join("\n");
}

const clip = (s, n) => (s.length > n ? `${s.slice(0, n)}…（已截断，原 ${s.length} 字）` : s);
const oneLine = (s) => String(s ?? "").replace(/\s+/g, " ").trim();

async function run(ctx, decls, limits, toolCallId, params, signal) {
  const base = { tool_call_id: toolCallId, name: NAME, label: LABEL, verb: VERB };
  // 开始前向 Go 报备：Go 计 1 步、在时间线里记「运行脚本」；步数用尽时不运行，停下等用户继续
  const started = await ctx.bridge.toolEvent({ phase: "start", ...base }, signal);
  if (started?.allowed === false) {
    ctx.onTerminate?.();
    return toResult(started.content || "已达到本轮的步数上限，运行已暂停，等待用户继续。", true, true);
  }

  const completed = []; // 已经成功的嵌套调用：失败时告诉模型哪些已经生效
  let nested = 0;
  const sandboxTools = decls.map((d) => ({
    name: d.name,
    description: d.description,
    inputSchema: d.parameters,
    execute: async (args, { signal: callSignal }) => {
      if (nested >= limits.maxNestedCalls) throw new Error(`脚本里调用工具的次数已达上限（${limits.maxNestedCalls} 次）`);
      nested++;
      const r = await ctx.bridge.tool(`${toolCallId}.${nested}`, d.name, wrapScalarArrays(d.parameters, args), callSignal, { parentToolCallId: toolCallId });
      if (r.is_error) throw new Error(r.content);
      if (r.terminate) ctx.onTerminate?.();
      completed.push({ name: d.name, note: clip(oneLine(r.content), 60) });
      return r.content;
    },
  }));

  const sandbox = new CodemodeSandbox({ tools: sandboxTools, timeoutMs: limits.timeoutMs, memoryLimitBytes: limits.memoryLimitBytes });
  let result;
  try {
    result = await sandbox.execute(String(params?.code ?? ""), { signal });
  } finally {
    await sandbox.close().catch(() => {});
  }

  const { text, isError } = format(result, completed, nested, limits);
  try {
    await ctx.bridge.toolEvent({ phase: "end", ...base, is_error: isError, summary: isError ? `脚本失败：${clip(oneLine(result.ok ? "" : result.error.message), 80)}` : `脚本调用了 ${nested} 次工具` });
  } catch (e) {
    ctx.log?.("报备脚本结束失败", { error: String(e?.message ?? e) }); // 只是时间线上少一条结束事件，不影响脚本的结果
  }
  return toResult(text, isError, false);
}

const toResult = (text, isError, terminate) => ({ content: [{ type: "text", text }], details: {}, isError, terminate });

function format(result, completed, nested, limits) {
  const output = result.output.filter((o) => o.type === "text").map((o) => o.text).join("\n");
  if (result.ok) {
    const parts = [];
    if (output) parts.push(clip(output, limits.maxResultChars));
    if (result.value !== undefined) parts.push("返回值：" + clip(JSON.stringify(result.value) ?? String(result.value), limits.maxResultChars));
    parts.push(`（脚本调用了 ${nested} 次工具）`);
    return { text: parts.join("\n"), isError: false };
  }
  const { kind, message } = result.error;
  const reason = {
    timeout: `脚本运行超时（超过 ${limits.timeoutMs / 1000} 秒），已被中止`,
    aborted: "脚本被中止",
    sandbox: `脚本沙箱出错：${message}`,
  }[kind] ?? `脚本出错：${message}`;
  const parts = [reason];
  if (output) parts.push("出错前的输出：\n" + clip(output, limits.maxResultChars));
  if (completed.length) {
    parts.push("出错前已完成的调用（它们已经生效，不要重复执行）：\n" + completed.map((c, i) => `${i + 1}. ${c.name}${c.note ? "：" + c.note : ""}`).join("\n"));
  }
  return { text: parts.join("\n"), isError: true };
}
