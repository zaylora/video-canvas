import { BACKGROUND_CONTEXT } from "@earendil-works/chord/context";
import type { AssistantMessage, Message, TranscriptContext } from "@earendil-works/pi-ai";
import { createModels } from "@earendil-works/pi-ai/models";
import { fauxAssistantMessage, fauxProvider, fauxText, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";

export const context = BACKGROUND_CONTEXT;
export { fauxAssistantMessage, fauxText, fauxToolCall };
export const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** 一条消息的一行文字 */
export function brief(m: Message): string {
  if (m.role === "system") return "system: (省略)";
  const c = typeof m.content === "string" ? [{ type: "text", text: m.content }] : m.content;
  const parts = (c as any[]).map((p) => (p.type === "text" ? p.text.replaceAll("\n", " ").slice(0, 60) : p.type === "toolCall" ? `toolCall:${p.name}(${JSON.stringify(p.arguments)})` : p.type));
  return `${m.role}${(m as any).stopReason && (m as any).stopReason !== "stop" ? "[" + (m as any).stopReason + "]" : ""}: ${parts.join(" | ")}`;
}

/** 假模型：记录每次请求看到的消息 */
export function makeModels(steps: any[], modelDef: { id: string; contextWindow?: number; maxTokens?: number } = { id: "faux-1" }) {
  const seen: Message[][] = [];
  const faux = fauxProvider({ models: [modelDef] });
  faux.setResponses(steps.map((s) => (typeof s === "function" ? async (t: TranscriptContext, ...rest: any[]) => { seen.push([...t.messages]); return s(t, ...rest); } : async (t: TranscriptContext) => { seen.push([...t.messages]); return s as AssistantMessage; })));
  const models = createModels();
  models.setProvider(faux.provider);
  return { models, seen, model: { provider: "faux", modelId: modelDef.id } };
}
