/**
 * 工具扩展：把 Go 下发的工具声明变成 Pi Durable 的工具。
 *
 * 工具的名字、说明、参数 schema 都由 Go 定（这里不内置任何业务工具），execute 回调 Go 的平台 API：
 *   - 普通工具 → POST /tools/:name，幂等键是 Pi 的工具任务 id（每次调用唯一，同一次调用重放时不变），
 *     所以崩溃后重放写画布不会写两遍，也不会因为模型给的 tool_call_id 重复而悄悄丢掉真实的写入；
 *   - 会让用户决定的三个（generate_media / canvas_delete / ask_user）→ 先 propose 预估，通过后交给宿主创建审批，本轮停下；
 *   - plan_update 在这里本地校验（它只产生一个事件，没有平台副作用）。
 *
 * 运行状态（步数、事件、审批、暂停）归运行引擎，工具层只通过 ToolHost 和它交互，所以可以单独测试。
 */
import type { Context } from "@earendil-works/chord";
import { defineExtension, type Extension, type ToolExecutionApi, type ToolRegistration } from "@earendil-works/pi-durable";
import { wrapScalarArrays } from "./tools.ts";
import { PlatformError, type ApprovalKind, type Platform, type PlatformToolResult, type RunContext, type ToolDecl, type ToolEndEvent } from "./platform-client.ts";

/** 工具所在的运行 */
export interface ToolRun {
  id: string;
  rc: RunContext;
}

/** 一次工具调用（tool.start 事件的内容加上参数） */
export interface ToolCallInfo {
  id: string;
  name: string;
  label: string;
  verb: string;
  /** 脚本（codemode）里发起的嵌套调用才有：外层调用的 id */
  parentId: string | undefined;
  args: unknown;
}

export interface ToolHost {
  /** 会话当前的运行；没有（运行已结束，迟到的调用）返回 undefined */
  runOf(conversationId: string): ToolRun | undefined;
  /** 本轮是否已经暂停（等审批、等回答、步数用尽）：同一轮里后面的调用不再执行 */
  isPaused(run: ToolRun): boolean;
  /** 工具开始：发 tool.start，检查步数上限。返回值表示拒绝执行（步数用尽等），工具层把 content 当结果交给模型 */
  begin(run: ToolRun, call: ToolCallInfo): Promise<{ content: string; terminate: boolean } | undefined>;
  /** 工具结束：发 tool.end（内容由 Go 给出），并计步数 */
  end(run: ToolRun, call: ToolCallInfo, event: ToolEndEvent): Promise<void>;
  /** 计划更新：发 plan.updated */
  plan(run: ToolRun, steps: PlanStep[]): Promise<void>;
  /** 创建审批并让运行进入等待状态。必须按 toolCallId 幂等：崩溃后重放同一个调用不能出现两个审批 */
  approval(run: ToolRun, a: { kind: ApprovalKind; toolCallId: string; payload: unknown; quoteCredits: number; summary: string }): Promise<void>;
}

export interface PlanStep {
  title: string;
  status: string;
}

/** 这几个工具要用户决定，对应的审批种类 */
const APPROVAL_KIND: Record<string, ApprovalKind> = { generate_media: "generate", canvas_delete: "delete", ask_user: "ask" };

/**
 * 崩溃后能不能重放：
 *  - 读工具、没有副作用：safe；
 *  - 改画布的写工具：Go 按幂等键去重（重放返回第一次的结果），所以 safe；
 *  - 审批类和计划：宿主按调用 id 幂等，safe；
 *  - 其余（截帧会写素材）和没登记的新工具：unsafe，崩溃后模型会收到「被中断，可能已经执行」，自己确认现状。
 */
const SAFE = new Set([
  "canvas_get_state", "model_list", "task_get", "skill_search", "skill_read", "canvas_inspect_image", "canvas_inspect_video", "web_search", "web_fetch",
  "canvas_apply_ops", "canvas_arrange", "plan_update", "generate_media", "canvas_delete", "ask_user",
]);
export const replayOf = (name: string): "safe" | "unsafe" => (SAFE.has(name) ? "safe" : "unsafe");

const MAX_PLAN_STEPS = 12;
const MAX_PLAN_TITLE = 80;
const SKIPPED_TEXT = "本轮已暂停等待用户，这条调用没有执行；用户回应后如仍需要请重新调用。";
const MAX_SUMMARY = 120;

export type Output = Array<{ type: "text"; text: string } | { type: "image"; data: string; mimeType: string }>;
export interface Result {
  output: Output;
  isError?: boolean;
  control?: { terminate: boolean };
}

const text = (t: string, isError = false, terminate = false): Result => ({ output: [{ type: "text", text: t }], ...(isError && { isError }), ...(terminate && { control: { terminate } }) });
export const clip = (s: string, n: number) => (Array.from(s).length > n ? Array.from(s).slice(0, n).join("") : s);

/** 脚本里发起的嵌套调用的调用 id 是 `<外层 id>/<key>`；返回外层 id */
export function parentOf(callId: string): string | undefined {
  const i = callId.lastIndexOf("/");
  return i > 0 ? callId.slice(0, i) : undefined;
}

export interface ToolsExtensionOptions {
  platform: Platform;
  /** Go 下发的工具声明，顺序即给模型的顺序（固定下来，提示词缓存才稳定） */
  decls: readonly ToolDecl[];
  host: ToolHost;
  name?: string;
}

export function createToolsExtension({ platform, decls, host, name = "canvas-tools" }: ToolsExtensionOptions): Extension {
  const tools: ToolRegistration[] = decls.map((d) => {
    if ((d.executor ?? "server") !== "server") throw new Error(`不支持的工具执行方：${d.name}（${d.executor}）`);
    return {
      name: d.name,
      description: d.description,
      // Go 下发的是普通 JSON Schema，pi 的校验接受它
      parameters: d.parameters as never,
      replay: replayOf(d.name),
      // 会创建审批并终止本轮的、返回图片的工具，脚本没法在「等审批」处停下，只能由模型直接调用
      callers: d.scriptable ? ["model", "tools"] : ["model"],
      // 同一轮里的多个调用必须一个个执行，否则同一条消息里的多个画布写入会并发
      executionMode: "sequential",
      // 模型常把 nodeIds 写成一个字符串；校验前补成数组，和迁移前的行为一致
      prepareArguments: (args: unknown) => wrapScalarArrays(d.parameters, args) as never,
      execute: (args: unknown, api: ToolExecutionApi, context: Context) => execute(d, args, api, context),
    } as ToolRegistration;
  });

  async function execute(d: ToolDecl, args: unknown, api: ToolExecutionApi, context: Context): Promise<Result> {
    return trackedCall(host, d, args, api, context, (run) => dispatch(d, args, run, api, context));
  }

  async function dispatch(d: ToolDecl, args: unknown, run: ToolRun, api: ToolExecutionApi, context: Context): Promise<{ result: Result; end: ToolEndEvent }> {
    const call = { id: api.callId, name: d.name, label: d.label, verb: d.verb };
    const kind = APPROVAL_KIND[d.name];
    if (kind) {
      const p = await platform.propose(run.rc, kind, args, context.abortSignal);
      if (p.is_error) return { result: text(p.content, true), end: endEvent(call, true, clip(p.content, MAX_SUMMARY)) };
      await host.approval(run, { kind, toolCallId: api.callId, payload: p.payload, quoteCredits: p.quote_credits, summary: p.summary ?? "" });
      return { result: text(p.content, false, p.terminate), end: endEvent(call, false, p.summary ?? "") };
    }
    if (d.name === "plan_update") return plan(call, run, args);
    const r = await platform.tool(run.rc, d.name, args, { toolCallId: api.callId, idemKey: String(api.taskId), signal: context.abortSignal });
    return { result: toResult(r), end: r.tool_end ?? endEvent(call, r.is_error, r.is_error ? clip(r.content, MAX_SUMMARY) : "") };
  }

  async function plan(call: Pick<ToolCallInfo, "id" | "name" | "label" | "verb">, run: ToolRun, args: unknown): Promise<{ result: Result; end: ToolEndEvent }> {
    const fail = (msg: string) => ({ result: text(msg, true), end: endEvent(call, true, clip(msg, MAX_SUMMARY)) });
    const raw = ((args as { steps?: Array<{ title?: unknown; status?: unknown }> } | undefined)?.steps ?? []) as Array<{ title?: unknown; status?: unknown }>;
    if (!Array.isArray(raw)) return fail("参数格式不对：steps 必须是数组");
    if (raw.length > MAX_PLAN_STEPS) return fail(`计划最多 ${MAX_PLAN_STEPS} 步`);
    const steps: PlanStep[] = [];
    for (const [i, s] of raw.entries()) {
      const title = typeof s?.title === "string" ? s.title.trim() : "";
      if (title === "" || Array.from(title).length > MAX_PLAN_TITLE) return fail(`第 ${i + 1} 步的标题不能为空，且不超过 ${MAX_PLAN_TITLE} 个字`);
      if (s.status !== "todo" && s.status !== "doing" && s.status !== "done") return fail(`第 ${i + 1} 步的状态只能是 todo / doing / done`);
      steps.push({ title, status: s.status });
    }
    await host.plan(run, steps);
    return { result: text("计划已更新。"), end: endEvent(call, false, `更新计划（${steps.length} 步）`) };
  }

  return defineExtension({ name, tools });
}

/**
 * 一次工具调用的完整流程：宿主 begin（发 tool.start、检查步数上限）→ 执行 → 宿主 end（发 tool.end、计步数）。
 * 服务端工具和 codemode（pi 自带的工具，同样要在时间线里留一步、计一步）共用。
 * body 返回 Pi 的工具结果和 tool.end 事件的内容；抛出的错误按「平台调用失败」处理，中止则原样抛出。
 */
export async function trackedCall(
  host: ToolHost,
  info: { name: string; label: string; verb: string },
  args: unknown,
  api: ToolExecutionApi,
  context: Context,
  body: (run: ToolRun) => Promise<{ result: Result; end: ToolEndEvent }>,
): Promise<Result> {
  const run = host.runOf(String(api.conversationId));
  if (!run) return text("这次运行已经结束，调用没有执行。", true);
  const parentId = parentOf(api.callId);
  // 本轮已暂停：后面的调用不执行，连 tool.start 都不发（嵌套调用属于已经开始的脚本，不受这个限制）
  if (!parentId && host.isPaused(run)) return text(SKIPPED_TEXT, true, true);
  const call: ToolCallInfo = { id: api.callId, name: info.name, label: info.label, verb: info.verb, parentId, args };
  const blocked = await host.begin(run, call);
  if (blocked) return text(blocked.content, true, blocked.terminate);

  let result: Result;
  let end: ToolEndEvent;
  try {
    ({ result, end } = await body(run));
  } catch (e) {
    if (context.abortSignal?.aborted) throw e; // 被中止：不是工具出错，让 pi 按中止处理
    // 平台调用本身失败（HTTP 500、连不上）：模型看到错误结果自己决定怎么办，时间线上这一步要收尾
    const msg = e instanceof PlatformError ? e.message : `工具执行出错：${String((e as Error)?.message ?? e)}`;
    result = text(msg, true);
    end = endEvent(call, true, clip(msg, MAX_SUMMARY));
  }
  await host.end(run, call, parentId ? { ...end, parent_id: parentId } : end);
  return result;
}

/** 工具自己（不是 Go）产生的 tool.end 事件：和 Go 组的字段一致 */
export function endEvent(call: Pick<ToolCallInfo, "id" | "name" | "label" | "verb">, isError: boolean, summary: string): ToolEndEvent {
  return { id: call.id, name: call.name, is_error: isError, summary, label: call.label, verb: call.verb };
}

function toResult(r: PlatformToolResult): Result {
  // 对象存储里的图片只有地址：放进 data，发请求前由模型提供方还原成 URL
  const images = (r.images ?? []).map((i) => ({ type: "image" as const, data: (i.url ?? i.data) as string, mimeType: i.mime_type }));
  return { output: [{ type: "text", text: r.content }, ...images], ...(r.is_error && { isError: true }), ...(r.terminate && { control: { terminate: true } }) };
}
