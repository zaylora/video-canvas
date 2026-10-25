import { createServer, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo } from "node:net";

/** 假的 Go 平台：记下每个请求，按路径返回预设的响应（标准信封 {code,msg,data}）。 */
export interface Seen {
  method: string;
  path: string;
  headers: IncomingMessage["headers"];
  body: unknown;
  raw: string;
}

export interface FakePlatform {
  url: string;
  seen: Seen[];
  /** 下一次命中 method+path 前缀的请求返回这个响应；不设就返回 {code:0,data:{}} */
  reply(match: string, res: { status?: number; json?: unknown; text?: string }): void;
  close(): Promise<void>;
}

export async function fakePlatform(): Promise<FakePlatform> {
  const seen: Seen[] = [];
  const replies: Array<{ match: string; res: { status?: number; json?: unknown; text?: string } }> = [];
  const server: Server = createServer((req, res) => {
    let raw = "";
    req.on("data", (d) => (raw += d));
    req.on("end", () => {
      let body: unknown;
      try {
        body = raw ? JSON.parse(raw) : undefined;
      } catch {
        body = raw;
      }
      const path = req.url ?? "";
      seen.push({ method: req.method ?? "", path, headers: req.headers, body, raw });
      const key = `${req.method} ${path}`;
      const idx = replies.findIndex((r) => key.startsWith(r.match));
      const r = idx >= 0 ? replies.splice(idx, 1)[0].res : { json: { code: 0, data: {} } };
      res.writeHead(r.status ?? 200, { "content-type": r.text !== undefined ? "text/html" : "application/json" });
      res.end(r.text !== undefined ? r.text : JSON.stringify(r.json));
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  return {
    url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
    seen,
    reply: (match, res) => void replies.push({ match, res }),
    close: () => new Promise((r) => server.close(() => r())),
  };
}
