// 测试用的子进程：打开引擎，发起一个运行，打印 started，然后一直活着直到被杀。
// 参数：数据库文件、假 Go 的地址、会话 id（逗号分隔表示在多个会话里各发起一个运行）、动作（start 发起一个运行 | recover 只打开引擎，让它按启动恢复去续跑）、消息
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { AgentEngine } from "../../src/engine/engine.ts";
import { createIdCodec } from "../../src/ids.ts";
import { createPlatform } from "../../src/platform-client.ts";
import { openStore } from "../../src/store/store.ts";

const [dbPath, goUrl, sessionId, action = "start", message = "拆分镜"] = process.argv.slice(2);
const TOKEN = "service-token-0123456789abcdef0123456789";
const store = openStore(dbPath, { workerId: 1 });
const engine = await AgentEngine.open({
  store, platform: createPlatform({ baseUrl: goUrl, serviceToken: TOKEN }), baseUrl: goUrl, serviceToken: TOKEN,
  storage: await openNodeSqliteStorage(dbPath, { prefix: "pi_" }), codec: createIdCodec("test-key"), leaseIntervalMs: 30,
});
const ids: string[] = [];
if (action === "start") for (const sid of sessionId.split(",")) ids.push((await engine.startRun({ userId: "1", sessionId: sid, message, modelKey: "claude" })).id);
process.stdout.write(`started ${ids.join(",") || "0"}\n`);
setInterval(() => {}, 1000);
