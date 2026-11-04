import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";

/** 试跑可等十分钟；Node fetch 默认五分钟的响应头超时会提前中断，故使用原生 HTTP。 */
export function waitForPlatform(url: string, init: RequestInit): Promise<Response> {
  return new Promise((resolve, reject) => {
    const target = new URL(url);
    const request = target.protocol === "https:" ? httpsRequest : httpRequest;
    const req = request(
      target,
      {
        method: init.method,
        headers: init.headers as Record<string, string>,
        signal: init.signal ?? undefined,
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
        res.on("error", reject);
        res.on("end", () => {
          const headers = new Headers();
          for (const [key, value] of Object.entries(res.headers))
            if (value !== undefined)
              headers.set(key, Array.isArray(value) ? value.join(", ") : value);
          resolve(new Response(Buffer.concat(chunks), { status: res.statusCode, headers }));
        });
      },
    );
    req.setTimeout(11 * 60_000, () => req.destroy(new Error("平台试跑等待超时")));
    req.on("error", reject);
    req.end(init.body);
  });
}
