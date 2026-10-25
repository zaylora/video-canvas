import { createServer } from "node:http";
import type { AddressInfo } from "node:net";

/** 假的 Go 模型代理（OpenAI 兼容的流式端点）：记下每个请求的路径、请求头和请求体，按预设回答，并带上 X-Agent-Call-Id 响应头。 */
export interface ModelRequest {
  path: string;
  headers: Record<string, string | string[] | undefined>;
  body: { model?: string; messages?: unknown[]; [k: string]: unknown };
}

export async function fakeModelProxy(answer = "好的") {
  const requests: ModelRequest[] = [];
  let nextCallId = 5_000_000_000_000_000_001n; // 超过 2^53
  const server = createServer((req, res) => {
    let raw = "";
    req.on("data", (d) => (raw += d));
    req.on("end", () => {
      requests.push({ path: req.url ?? "", headers: req.headers, body: JSON.parse(raw || "{}") });
      res.writeHead(200, { "content-type": "text/event-stream", "x-agent-call-id": String(nextCallId++) });
      const chunk = (delta: object, finish: string | null = null) =>
        `data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`;
      res.write(chunk({ role: "assistant", content: answer }));
      res.write(chunk({}, "stop"));
      res.write(`data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [], usage: { prompt_tokens: 10, completion_tokens: 2, total_tokens: 12 } })}\n\n`);
      res.end("data: [DONE]\n\n");
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  return {
    url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
    requests,
    close: () => new Promise<void>((r) => server.close(() => r())),
  };
}
