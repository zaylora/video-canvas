#!/usr/bin/env node
import { configFromEnv, startDurable } from "./durable.ts";

/**
 * Agent 服务的入口：Pi Durable 引擎 + 前端用的 REST 接口，会话、运行、审批、事件都在这里，数据在 SQLite（含 Pi 自己的表）。
 * 用环境变量配置（缺哪一项、哪一项不对，启动时直接退出并说清楚），详见 durable.ts 的 configFromEnv：
 *   AGENT_SERVICE_TOKEN          必填，至少 32 个字符；Go 调这个服务、这个服务调 Go 的桥，都靠它
 *   AGENT_DB_PATH                必填，SQLite 文件（放在本地卷上，不要放网络盘）
 *   AGENT_BRIDGE_URL             必填，Go 的桥地址，如 http://backend:47001
 *   AGENT_ID_KEY                 必填，对外 ID 的编码密钥，必须和 Go 实际使用的一样（启动后会向 Go 核对，不一致 /health 报 503）
 *   AGENT_LISTEN                 监听地址，默认 0.0.0.0:8090；写 127.0.0.1:0 表示本机随机端口（测试用）
 *   AGENT_WORKER_ID              雪花 ID 的节点号 0..1023，默认 1
 *   AGENT_RESUME_WINDOW_SECONDS  重启后自动续跑的窗口，默认 300
 * 日志写 stderr（JSON 行）。收到 SIGTERM / SIGINT 只关闭服务和存储，不中止运行：运行留在原状态，下次启动按窗口续跑或标为中断。
 */
const log = (msg: string, extra: Record<string, unknown> = {}) => process.stderr.write(JSON.stringify({ msg, ...extra }) + "\n");

/** 启动前自检失败：说清楚原因就退出，不要等到第一次请求才发现 */
function die(msg: string): never {
  log(msg);
  process.exit(1);
}

let svc;
try {
  svc = await startDurable(configFromEnv(process.env), log);
} catch (e) {
  die(String((e as Error)?.message ?? e));
}
log("Agent 服务已启动", { addr: svc.url.replace(/^https?:\/\//, "") });
process.on("unhandledRejection", (e) => log("未处理的 Promise 异常", { error: String((e as Error)?.message ?? e) }));
let stopping = false;
for (const sig of ["SIGTERM", "SIGINT"] as const) {
  process.on(sig, () => {
    if (stopping) return;
    stopping = true;
    log("收到停机信号，关闭服务（运行留在原状态，下次启动按窗口续跑）", { signal: sig });
    // 容器的停止宽限期是 30 秒，留 5 秒给进程退出
    const timer = setTimeout(() => process.exit(1), 25_000);
    svc.close().then(() => process.exit(0), () => process.exit(1)).finally(() => clearTimeout(timer));
  });
}
