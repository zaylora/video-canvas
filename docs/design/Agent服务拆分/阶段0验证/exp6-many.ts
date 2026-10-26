import { mkdtempSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createRegistry, Harness } from "@earendil-works/pi-durable";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { brief, context, fauxAssistantMessage, makeModels } from "./common.ts";

// 一个 Harness（一个 SQLite 文件）托管很多会话，同时各自提问：看是否都完成、互不串、耗时和文件大小
const N = Number(process.argv[2] ?? 50);
const TURNS = 3;
const dir = mkdtempSync(join(tmpdir(), "pd-many-"));
const file = join(dir, "s.sqlite");
// 每个请求回显它看到的最后一条用户消息：会话之间串了内容就能看出来
const echo = (t: any) => {
  const last = [...t.messages].reverse().find((m: any) => m.role === "user") as any;
  return new Promise<any>((r) => setTimeout(() => r(fauxAssistantMessage(`答：${last.content[0]?.text ?? last.content}`)), 20 + Math.random() * 30));
};
const { models, model } = makeModels(Array.from({ length: N * TURNS + 10 }, () => echo));
const harness = await Harness.open(await openNodeSqliteStorage(file), { models, registry: createRegistry() }, context);
const convs = await Promise.all(Array.from({ length: N }, () => harness.createConversation({ ownership: { kind: "ownerless" }, agent: { model } } as any, context)));

const t0 = Date.now();
let wrong = 0, notDone = 0;
await Promise.all(convs.map(async (c, i) => {
  for (let k = 0; k < TURNS; k++) {
    const rec = await (await c.submit({ type: "input", content: `会话${i}-第${k}问` }, context)).wait(context);
    if (rec.status !== "done") notDone++;
  }
  const items = [...(await c.entries({}, 50, undefined, context)).items].reverse();
  const answers = items.filter((e) => e.kind === "pi.assistant").map((e) => (e.model![0] as any).content[0].text);
  // 模型回显的是「它看到的最后一条用户消息」，应当恰好是自己这一问
  answers.forEach((a, k) => { if (a !== `答：会话${i}-第${k}问`) wrong++; });
}));
const ms = Date.now() - t0;
console.log(`会话 ${N} 个 × ${TURNS} 轮，同时进行：耗时 ${ms}ms，未完成 ${notDone}，串内容 ${wrong}`);
console.log(`进程内存 RSS ${(process.memoryUsage().rss / 1048576).toFixed(0)}MB`);
await harness.close(context);
console.log(`SQLite 文件 ${(statSync(file).size / 1024).toFixed(0)}KB（含 -wal 之外的主文件）`);
rmSync(dir, { recursive: true, force: true });
