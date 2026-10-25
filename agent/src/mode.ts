/**
 * 服务入口的两种模式：durable（Pi Durable 引擎 + 前端用的 REST 接口，会话、运行、审批都在这里）和 legacy（旧的无状态服务，Go 逐个启动运行片段）。
 *
 * 由 AGENT_RUNTIME 显式决定（Compose 里直接传 backend 的 APP_AGENT_RUNTIME）：durable 是新引擎，http / process 是旧的。
 * 不能只看 AGENT_DB_PATH：Compose 里数据路径是恒定设置的，回退到 http 时不能因为有路径就进了 durable。
 * 没设 AGENT_RUNTIME 时沿用旧规则：配了 AGENT_DB_PATH 就是 durable。
 */
export type ServiceMode = "durable" | "legacy";

export function serviceMode(env: Record<string, string | undefined>): ServiceMode {
  const runtime = env.AGENT_RUNTIME ?? "";
  const hasDb = (env.AGENT_DB_PATH ?? "") !== "";
  switch (runtime) {
    case "":
      return hasDb ? "durable" : "legacy";
    case "durable":
      if (!hasDb) throw new Error("AGENT_RUNTIME=durable 需要 AGENT_DB_PATH（SQLite 文件，放在本地卷上）");
      return "durable";
    case "http":
    case "process":
      return "legacy";
    default:
      throw new Error(`AGENT_RUNTIME 只能是 durable、http 或 process，当前是 ${JSON.stringify(runtime)}`);
  }
}
