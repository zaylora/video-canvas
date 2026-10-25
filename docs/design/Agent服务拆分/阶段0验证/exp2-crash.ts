import { spawn } from "node:child_process";
import { appendFileSync, existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Type } from "@earendil-works/pi-ai";
import { createRegistry, defineExtension, defineTool, Harness } from "@earendil-works/pi-durable";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { brief, context, fauxAssistantMessage, fauxToolCall, makeModels, sleep } from "./common.ts";

const [, , role, db, replay, counter] = process.argv;

if (role === "child" || role === "recover") {
  const Tool = defineTool({
    name: "write_canvas",
    description: "改画布",
    parameters: Type.Object({ n: Type.Number() }),
    replay: replay as "safe" | "never",
    execute: async (args) => {
      appendFileSync(counter, `exec ${role} n=${args.n}\n`);
      if (role === "child") await new Promise(() => {}); // 第一次：卡在工具里，等着被杀
      return { output: [{ type: "text", text: `画布已改（第二次执行，n=${args.n}）` }] };
    },
  });
  const registry = createRegistry();
  registry.install(defineExtension({ name: "canvas", tools: [Tool] }));
  const steps = role === "child"
    ? [fauxAssistantMessage([fauxToolCall("write_canvas", { n: 1 }, { id: "c1" })], { stopReason: "toolUse" }), fauxAssistantMessage("收尾完成")]
    : [fauxAssistantMessage("收尾完成")];
  const { models, seen, model } = makeModels(steps);
  const harness = await Harness.open(await openNodeSqliteStorage(db), { models, registry }, context);
  const root = await harness.root(context, { agent: { model } });
  if (role === "child") {
    await root.submit({ type: "input", content: "改一下画布" }, context);
    await sleep(60_000);
  } else {
    harness.resume(); // 接着跑上次没跑完的
    for (let i = 0; i < 100; i++) {
      const items = (await root.entries({}, 20, undefined, context)).items;
      if (items[0]?.kind === "pi.assistant" && seen.length > 0) break;
      await sleep(50);
    }
    console.log("  重开后模型请求了", seen.length, "次，看到的上下文：");
    for (const m of seen.at(-1) ?? []) console.log("    ", brief(m));
    const kinds = [...(await root.entries({}, 20, undefined, context)).items].reverse().map((e) => e.kind);
    console.log("  最终记录:", kinds.join(" → "));
    await harness.close(context);
  }
} else {
  // 编排：每种 replay 设置各跑一次「杀进程 → 重开」
  for (const mode of ["never", "safe"]) {
    const dir = mkdtempSync(join(tmpdir(), "pd-crash-"));
    const dbPath = join(dir, "s.sqlite");
    const cnt = join(dir, "count.txt");
    appendFileSync(cnt, "");
    console.log(`\n== replay: "${mode}"`);
    const child = spawn("node", ["--disable-warning=ExperimentalWarning", import.meta.filename, "child", dbPath, mode, cnt], { stdio: "inherit" });
    for (let i = 0; i < 200 && !readFileSync(cnt, "utf8").includes("exec"); i++) await sleep(25);
    child.kill("SIGKILL");
    await new Promise((r) => child.on("close", r));
    console.log("  已 SIGKILL，工具执行记录:", JSON.stringify(readFileSync(cnt, "utf8").trim()));
    await new Promise((r) => spawn("node", ["--disable-warning=ExperimentalWarning", import.meta.filename, "recover", dbPath, mode, cnt], { stdio: "inherit" }).on("close", r));
    console.log("  工具总执行次数:", readFileSync(cnt, "utf8").trim().split("\n").length);
    rmSync(dir, { recursive: true, force: true });
  }
}
