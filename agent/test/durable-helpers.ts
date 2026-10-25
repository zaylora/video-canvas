import { BACKGROUND_CONTEXT } from "@earendil-works/chord/context";
import type { AssistantMessage, Message, TranscriptContext } from "@earendil-works/pi-ai";
import { createModels } from "@earendil-works/pi-ai/models";
import { fauxAssistantMessage, fauxProvider, fauxText, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";

export const context = BACKGROUND_CONTEXT;
export { fauxAssistantMessage, fauxText, fauxToolCall };
export const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** 一轮模型回答里调一个或几个工具 */
export const callTools = (...calls: Array<{ id: string; name: string; args?: Record<string, any> }>) =>
  fauxAssistantMessage(calls.map((c) => fauxToolCall(c.name, c.args ?? {}, { id: c.id })), { stopReason: "toolUse" });

/** 假模型：按顺序给出预设的回答，记录每次请求看到的消息 */
export function fauxModels(steps: Array<AssistantMessage | ((t: TranscriptContext) => AssistantMessage)>, def: { id: string; contextWindow?: number } = { id: "faux-1" }) {
  const seen: Message[][] = [];
  const faux = fauxProvider({ models: [def] });
  faux.setResponses(steps.map((s) => async (t: TranscriptContext) => {
    seen.push([...t.messages]);
    return typeof s === "function" ? s(t) : s;
  }));
  const models = createModels();
  models.setProvider(faux.provider);
  return { models, seen, model: { provider: "faux", modelId: def.id } };
}

/** 一条消息里的文字（工具结果、用户消息） */
export function textOf(m: Message): string {
  if (typeof m.content === "string") return m.content;
  return (m.content as Array<{ type: string; text?: string }>).map((p) => (p.type === "text" ? p.text : `[${p.type}]`)).join("");
}
