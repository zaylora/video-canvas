import { execFile, spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

// 新测试共用的小工具：用真实 HTTP 的假站点、独立凭据目录、异步跑一次 CLI。
// 断言的是「CLI 实际发了什么」，所以假站点会记下每个请求。

const here = path.dirname(fileURLToPath(import.meta.url));
export const bin = path.join(here, "..", "..", "dist", "cli.js");

/** 测试里断言的 JSON 体：结构随用例变，不在这里逐一建模。 */
export type Json = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

export interface Recorded {
  method: string;
  path: string;
  query: URLSearchParams;
  headers: http.IncomingHttpHeaders;
  body: Json;
  raw: string;
}

export interface Reply {
  status?: number;
  body: unknown;
  contentType?: string;
}

export type Handler = (req: Recorded, count: number) => Reply | undefined;

export interface Site {
  base: string;
  requests: Recorded[];
  close(): Promise<void>;
}

/** 起一个假站点：handler 返回 undefined 时回 404。 */
export async function startSite(handler: Handler): Promise<Site> {
  const requests: Recorded[] = [];
  const server = http.createServer((req, res) => {
    let raw = "";
    req.on("data", (c: Buffer) => (raw += c.toString()));
    req.on("end", () => {
      const url = new URL(req.url ?? "/", "http://x");
      const recorded: Recorded = {
        method: req.method ?? "GET",
        path: url.pathname,
        query: url.searchParams,
        headers: req.headers,
        body:
          raw && !req.headers["content-type"]?.startsWith("multipart/")
            ? (JSON.parse(raw) as Json)
            : {},
        raw,
      };
      requests.push(recorded);
      const reply = handler(recorded, requests.length) ?? {
        status: 404,
        body: { code: 404, msg: "not found" },
      };
      res.writeHead(reply.status ?? 200, {
        "content-type": reply.contentType ?? "application/json",
      });
      res.end(Buffer.isBuffer(reply.body) ? reply.body : JSON.stringify(reply.body));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const addr = server.address();
  const port = typeof addr === "object" && addr ? addr.port : 0;
  return {
    base: `http://127.0.0.1:${port}`,
    requests,
    close: () =>
      new Promise<void>((resolve) => {
        server.closeAllConnections?.();
        server.close(() => resolve());
      }),
  };
}

export const ok = (data: unknown, status = 200): Reply => ({
  status,
  body: { code: 0, msg: "success", data },
});

/** 独立配置目录；传了 creds 就写一份登录态（base 为站点根，自动补 /api/v1）。 */
export function tempConfig(creds?: {
  base?: string;
  token?: string;
  [k: string]: unknown;
}): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "vc-t-"));
  if (creds) {
    fs.mkdirSync(path.join(root, "video-canvas"), { recursive: true });
    const body = { role: "user", ...creds, base: creds.base ? `${creds.base}/api/v1` : undefined };
    fs.writeFileSync(path.join(root, "video-canvas", "credentials.json"), JSON.stringify(body));
  }
  return root;
}

export interface RunResult {
  code: number | null;
  stdout: string;
  stderr: string;
}

/** 异步跑一次 CLI：假站点在本进程里，spawnSync 会卡死事件循环。 */
export function runCli(
  args: string[],
  env: Record<string, string> = {},
  options: { cwd?: string; input?: string } = {},
): Promise<RunResult> {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [bin, ...args], {
      cwd: options.cwd,
      env: { ...process.env, VC_NO_UPDATE_CHECK: "1", VC_RETRY_BASE_MS: "20", ...env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (c: Buffer) => (stdout += c.toString()));
    child.stderr.on("data", (c: Buffer) => (stderr += c.toString()));
    child.on("close", (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(options.input ?? "");
  });
}

const python = ((): string | null => {
  for (const name of ["python3", "python"]) {
    if (spawnSync(name, ["-c", "import pty"], { encoding: "utf8" }).status === 0) return name;
  }
  return null;
})();

/** 有没有 python3：伪终端只能靠它造；没有就跳过交互确认的用例。 */
export const hasPty = python !== null;

/** 用伪终端跑一次，模拟真人敲字（只用于需要终端确认的用例）。 */
export function runInPty(
  args: string[],
  env: Record<string, string>,
  input: string,
): Promise<RunResult> {
  const helper = path.join(here, "pty_run.py");
  return new Promise((resolve) => {
    execFile(
      python as string,
      [helper, process.execPath, bin, ...args],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          VC_NO_UPDATE_CHECK: "1",
          VC_RETRY_BASE_MS: "20",
          ...env,
          PTY_INPUT: input,
        },
      },
      (err, stdout, stderr) =>
        resolve({
          code: err ? Number(err.code ?? 1) : 0,
          stdout: String(stdout),
          stderr: String(stderr),
        }),
    );
  });
}
