import { createApiApp } from "../src/api/http.ts";
import { AgentApiService } from "../src/api/service.ts";
import { codec, TOKEN, withEngine, type Env } from "./engine-helpers.ts";
import type { FakeGoOptions } from "./fake-go.ts";

export interface Http {
  url: string;
  /** 带服务令牌和用户的请求；headers 里可覆盖 */
  call(method: string, path: string, body?: unknown, headers?: Record<string, string | undefined>): Promise<{ status: number; body: any }>;
}

export async function withHttp(go: FakeGoOptions, fn: (h: Http, e: Env) => Promise<void>, over: { maxBody?: number } = {}) {
  await withEngine(go, async (e) => {
    const service = new AgentApiService({ store: e.store, engine: e.engine, platform: e.platform, codec });
    const app = createApiApp({ service, codec, serviceToken: TOKEN, log: () => {}, ...over });
    const url = await app.listen({ port: 0, host: "127.0.0.1" });
    const call: Http["call"] = async (method, path, body, headers = {}) => {
      const h: Record<string, string> = { "x-agent-service-token": TOKEN, "x-agent-user": "1", ...(body !== undefined && { "content-type": "application/json" }) };
      for (const [k, v] of Object.entries(headers)) if (v === undefined) delete h[k]; else h[k] = v;
      const res = await fetch(url + path, { method, headers: h, body: body === undefined ? undefined : typeof body === "string" ? body : JSON.stringify(body) });
      return { status: res.status, body: await res.json().catch(() => null) };
    };
    try {
      await fn({ url, call }, e);
    } finally {
      await app.close();
    }
  });
}
