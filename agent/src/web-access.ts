/** Agent 直接管理 pi-web-access MCP：缓存按用户和会话隔离，供应商凭据不会经过 Go。 */
import { mkdirSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import type { RunContext } from "./platform-client.ts";

export interface WebResult {
  content: Array<{ type: string; text?: string }>;
  isError?: boolean;
}
export interface WebConnection {
  callTool(name: string, args: Record<string, unknown>, signal?: AbortSignal): Promise<WebResult>;
  close(): Promise<void>;
}
export interface WebTools {
  call(
    rc: Pick<RunContext, "user_id" | "session_id">,
    name: string,
    args: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<WebResult>;
  close(): Promise<void>;
}
export interface WebAccessOptions {
  dataDir: string;
  allowFakeIP?: boolean;
  env?: Record<string, string | undefined>;
  /** 测试替身；生产固定启动锁定版本的 pi-web-access，不接受模型提供的命令。 */
  connect?: (profile: string, env: Record<string, string>) => Promise<WebConnection>;
}

const MAX_SESSIONS = 8;
const IDLE_MS = 15 * 60_000;
// 开发时依赖挂在 Windows 文件系统上，MCP 冷启动可能超过 10 秒；握手等待独立于网页请求的时限。
const STARTUP_TIMEOUT_MS = 60_000;
const TIMEOUT_MS = 65_000;
const CHILD_ENV = [
  "PATH",
  "SystemRoot",
  "WINDIR",
  "TEMP",
  "TMP",
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "ALL_PROXY",
  "NO_PROXY",
  "EXA_API_KEY",
  "BRAVE_API_KEY",
  "TAVILY_API_KEY",
  "FIRECRAWL_API_KEY",
  "SEARXNG_BASE_URL",
];
type Entry = { ready: Promise<WebConnection>; busy: number; touched: number };

/** 一个会话一个进程：插件的 responseId 缓存在进程内，不能让其他用户查询。 */
export class WebAccess implements WebTools {
  #options: WebAccessOptions;
  #sessions = new Map<string, Entry>();
  #closed = false;
  #timer: ReturnType<typeof setInterval>;

  constructor(options: WebAccessOptions) {
    this.#options = { ...options, dataDir: resolve(options.dataDir) };
    this.#timer = setInterval(() => {
      for (const [key, entry] of this.#sessions) {
        if (!entry.busy && Date.now() - entry.touched > IDLE_MS) this.#discard(key, entry);
      }
    }, 60_000);
    this.#timer.unref();
  }

  async call(
    rc: Pick<RunContext, "user_id" | "session_id">,
    name: string,
    args: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<WebResult> {
    signal?.throwIfAborted();
    if (this.#closed) throw new Error("联网服务已关闭。");
    if (!/^\d+$/.test(rc.user_id) || !/^\d+$/.test(rc.session_id))
      throw new Error("联网会话标识不合法。");
    const key = `${rc.user_id}/${rc.session_id}`;
    let entry = this.#sessions.get(key);
    if (!entry) {
      if (this.#sessions.size >= MAX_SESSIONS) {
        const idle = [...this.#sessions]
          .filter(([, e]) => !e.busy)
          .sort((a, b) => a[1].touched - b[1].touched)[0];
        if (!idle) throw new Error("当前联网请求较多，请稍后再试。");
        this.#discard(...idle);
      }
      entry = { ready: this.#connect(key), busy: 0, touched: Date.now() };
      this.#sessions.set(key, entry);
    }
    entry.busy++;
    let connected = false;
    try {
      const connection = await entry.ready;
      connected = true;
      signal?.throwIfAborted();
      return await connection.callTool(name, args, signal);
    } catch (error) {
      await this.#discard(key, entry);
      signal?.throwIfAborted();
      if (typeof error === "object" && error !== null && "code" in error && error.code === -32001) {
        throw new Error(
          connected
            ? "联网请求超时，网站或搜索服务未及时响应，请稍后重试。"
            : "联网工具启动超时，本机首次加载可能较慢，请稍后重试。",
        );
      }
      // SDK 和供应商错误可能含 URL 或凭据，不把原异常交给模型或时间线。
      throw new Error(
        "联网服务暂不可用，请稍后重试；之前的内容缓存可能已失效，请重新搜索或读取网页。",
      );
    } finally {
      entry.busy--;
      entry.touched = Date.now();
    }
  }

  async close(): Promise<void> {
    this.#closed = true;
    clearInterval(this.#timer);
    await Promise.allSettled([...this.#sessions].map(([key, entry]) => this.#discard(key, entry)));
  }

  /** 先移除再关闭：新调用不会复用已断开的连接；关闭也会回收 stdio 子进程。 */
  async #discard(key: string, entry: Entry): Promise<void> {
    if (this.#sessions.get(key) !== entry) return;
    this.#sessions.delete(key);
    await entry.ready.then((c) => c.close()).catch(() => {});
  }

  async #connect(key: string): Promise<WebConnection> {
    const profile = join(this.#options.dataDir, ...key.split("/"));
    const source = this.#options.env ?? process.env;
    // 已配置的独立额度优先，不让有 Key 的部署仍先消耗公共 Exa 免费额度。
    const provider = source.EXA_API_KEY
      ? "exa"
      : source.BRAVE_API_KEY
        ? "brave"
        : source.TAVILY_API_KEY
          ? "tavily"
          : source.FIRECRAWL_API_KEY
            ? "firecrawl"
            : "auto";
    mkdirSync(profile, { recursive: true, mode: 0o700 });
    writeFileSync(
      join(profile, "web-search.json"),
      JSON.stringify({
        provider,
        maxInlineContentChars: 20000,
        allowBrowserCookies: false,
        githubClone: { enabled: false },
        githubPrIssue: { enabled: false },
        youtube: { enabled: false },
        video: { enabled: false },
        pdf: { enabled: false },
        image: { enabled: false },
        tools: { sourceCheck: { enabled: false } },
        fetchRouting: { allowRemoteHostedProviders: false },
        ...(this.#options.allowFakeIP && { ssrf: { allowRanges: ["198.18.0.0/15"] } }),
      }),
      { mode: 0o600 },
    );
    const env: Record<string, string> = {
      HOME: profile,
      USERPROFILE: profile,
      XDG_CONFIG_HOME: profile,
      PI_CODING_AGENT_DIR: profile,
      PI_WEB_ACCESS_CACHE_ROOT: profile,
    };
    for (const name of CHILD_ENV) if (source[name]) env[name] = source[name]!;
    return (this.#options.connect ?? connectMcp)(profile, env);
  }
}

/** 启动已安装的编译入口，不使用 npx，也不在运行时下载或执行 TS 源码。 */
async function connectMcp(profile: string, env: Record<string, string>): Promise<WebConnection> {
  const root = dirname(createRequire(import.meta.url).resolve("pi-web-access/package.json"));
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [join(root, "dist/mcp-cli.js")],
    cwd: profile,
    env,
    stderr: "ignore",
  });
  const client = new Client({ name: "video-canvas-agent", version: "1.0.0" });
  try {
    await client.connect(transport, { timeout: STARTUP_TIMEOUT_MS });
    return {
      callTool: async (name, args, signal) =>
        (await client.callTool({ name, arguments: args }, undefined, {
          signal,
          timeout: TIMEOUT_MS,
        })) as WebResult,
      close: () => client.close(),
    };
  } catch (e) {
    await client.close().catch(() => {});
    throw e;
  }
}
