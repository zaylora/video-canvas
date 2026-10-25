// 重启后的运行怎么办（设计文档 18.6 / 20.6）：
//   S1 正常停机（close）时正在执行写工具，重开后 resume
//   S2 正常停机时正在等模型回答，重开后 resume
//   S3 「过期」的运行：重开后先 inspect，再对会话 abort，然后 resume；用户点「继续」= 提交一条新输入
//   S4 重开后既不 resume 也不 abort，直接提交新输入
//   S5 连续崩溃：任务被反复打断到重启上限后的表现
import { spawn } from "node:child_process";
import { appendFileSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Type } from "@earendil-works/pi-ai";
import { createRegistry, defineExtension, defineTool, Harness } from "@earendil-works/pi-durable";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { brief, context, fauxAssistantMessage, fauxToolCall, makeModels, sleep } from "./common.ts";

const SELF = import.meta.filename;
const NODE = ["--disable-warning=ExperimentalWarning", SELF];
const [, , role, scenario, db, marks] = process.argv;
const mark = (s: string) => appendFileSync(marks, s + "\n");
const read = (f: string) => readFileSync(f, "utf8");
/** 一个任务在 inspect 里的一行描述：定义名、记录状态、重启次数、调度器打算怎么处理它 */
const taskLine = (t: any) => `${t.record.name ?? t.record.kind ?? "?"}:${t.record.state?.status}${t.record.restarts ? `(restarts=${t.record.restarts})` : ""}→${t.state.kind}${t.state.reason ? `/${t.state.reason}` : ""}`;
const json = (v: unknown) => JSON.stringify(v, (_k, x) => (typeof x === "bigint" ? x.toString() : x));

async function child() {
  const hang = role === "first" || role === "again"; // 第一次（和 S5 里每次）会卡住，等着被停或被杀
  const Tool = defineTool({
    name: "write_canvas",
    description: "改画布",
    parameters: Type.Object({ n: Type.Number() }),
    replay: scenario === "S5" ? "safe" : "never",
    execute: async (args) => {
      mark(`tool-exec ${role}`);
      if (hang) await new Promise(() => {});
      return { output: [{ type: "text", text: `画布已改 n=${args.n}` }] };
    },
  });
  const registry = createRegistry();
  registry.install(defineExtension({ name: "canvas", tools: [Tool] }));
  const toolTurn = fauxAssistantMessage([fauxToolCall("write_canvas", { n: 1 }, { id: "c1" })], { stopReason: "toolUse" });
  const steps: any[] = hang || role === "continue-after-abort"
    ? scenario === "S2" && hang
      ? [async () => { mark("model-request first"); await new Promise(() => {}); }]
      : [toolTurn, fauxAssistantMessage("收尾完成")]
    : [fauxAssistantMessage("收尾完成")];
  const { models, seen, model } = makeModels(steps);
  const harness = await Harness.open(await openNodeSqliteStorage(db), { models, registry }, context);
  const root = await harness.root(context, { agent: { model } });
  const dump = async (label: string) => {
    const items = [...(await root.entries({}, 30, undefined, context)).items].reverse().map((e) => e.kind);
    console.log(`  ${label} 记录: ${items.join(" → ")}`);
  };

  if (role === "first" || role === "again") {
    await root.submit({ type: "input", content: "改一下画布" }, context);
    process.on("SIGTERM", async () => { // 正常停机：只关闭存储，不 abort
      await harness.close(context);
      mark("closed");
      process.exit(0);
    });
    await sleep(60_000);
    return;
  }

  // 重开后的进程
  const insp = await harness.inspect(context);
  console.log(`  重开后 inspect: scheduling=${insp.scheduling} 任务=${insp.tasks.map(taskLine).join(", ") || "无"}`);
  const waitDone = async (ms: number) => {
    for (let i = 0; i < ms / 50; i++) {
      const items = (await root.entries({}, 20, undefined, context)).items;
      if (items[0]?.kind === "pi.assistant" && seen.length > 0) return true;
      await sleep(50);
    }
    return false;
  };
  if (role === "recover") {
    harness.resume();
    const done = await waitDone(3000);
    console.log(`  resume 后${done ? "完成" : "3 秒内没有完成"}，模型请求 ${seen.length} 次`);
    for (const m of seen.at(-1) ?? []) console.log("    ", brief(m));
  } else if (role === "abort-then-continue") {
    await root.abort(context);
    const after = await harness.inspect(context);
    console.log(`  对会话 abort 后: 任务=${after.tasks.map(taskLine).join(", ") || "无"}`);
    await dump("abort 后");
    const sub = await root.submit({ type: "input", content: "继续" }, context);
    harness.resume();
    const done = await waitDone(3000);
    console.log(`  提交「继续」后${done ? "完成" : "3 秒内没有完成"}，提交状态 ${json((await sub.wait?.(context).catch((e: Error) => e.message)) ?? "")}`.slice(0, 200));
    for (const m of seen.at(-1) ?? []) console.log("    ", brief(m));
  } else if (role === "submit-without-resume") {
    const sub = await root.submit({ type: "input", content: "继续" }, context).catch((e: Error) => { console.log("  提交被拒绝:", e.message); return undefined; });
    if (sub) {
      const done = await waitDone(3000);
      console.log(`  直接提交「继续」${done ? "完成了" : "3 秒内没有完成"}，模型请求 ${seen.length} 次`);
      for (const m of seen.at(-1) ?? []) console.log("    ", brief(m));
    }
  } else if (role === "loop-probe") {
    harness.resume();
    await sleep(800);
    const now = await harness.inspect(context);
    console.log(`  resume 后 800ms: ${now.tasks.map(taskLine).join(", ") || "无"}`);
    // 试 retryTask
    const blocked = now.tasks.find((t: any) => t.state.kind === "blocked");
    if (blocked) {
      await harness.retryTask(blocked.record.id, context);
      await sleep(500);
      console.log(`  retryTask 后: ${read(marks).trim().split("\n").filter((l) => l.startsWith("tool-exec")).length} 次工具执行（累计）`);
    }
  }
  await dump("最终");
  await harness.close(context);
}

async function run(role_: string, scenario_: string, dbPath: string, markFile: string, signal?: NodeJS.Signals) {
  const p = spawn("node", [...NODE, role_, scenario_, dbPath, markFile], { stdio: "inherit" });
  if (signal) {
    const want = scenario_ === "S2" ? "model-request" : "tool-exec";
    for (let i = 0; i < 300 && !read(markFile).includes(want); i++) await sleep(25);
    await sleep(100);
    p.kill(signal);
  }
  await new Promise((r) => p.on("close", r));
}

async function orchestrate() {
  const fresh = () => {
    const dir = mkdtempSync(join(tmpdir(), "pd-restart-"));
    const m = join(dir, "marks.txt");
    appendFileSync(m, "");
    return { dir, db: join(dir, "s.sqlite"), m };
  };
  const execs = (m: string) => read(m).split("\n").filter((l) => l.startsWith("tool-exec")).length;

  console.log("\n== S1 正常停机（SIGTERM → close）时在执行写工具，重开后 resume");
  let t = fresh();
  await run("first", "S1", t.db, t.m, "SIGTERM");
  console.log(`  停机记录: ${read(t.m).trim().split("\n").join(", ")}`);
  await run("recover", "S1", t.db, t.m);
  console.log(`  工具总执行次数: ${execs(t.m)}`);
  rmSync(t.dir, { recursive: true, force: true });

  console.log("\n== S2 正常停机时正在等模型回答，重开后 resume");
  t = fresh();
  await run("first", "S2", t.db, t.m, "SIGTERM");
  console.log(`  停机记录: ${read(t.m).trim().split("\n").join(", ")}`);
  await run("recover", "S2", t.db, t.m);
  rmSync(t.dir, { recursive: true, force: true });

  console.log("\n== S3 过期的运行：SIGKILL 后重开，先 abort 会话，再提交「继续」");
  t = fresh();
  await run("first", "S3", t.db, t.m, "SIGKILL");
  await run("abort-then-continue", "S3", t.db, t.m);
  console.log(`  工具总执行次数: ${execs(t.m)}`);
  rmSync(t.dir, { recursive: true, force: true });

  console.log("\n== S4 SIGKILL 后重开，既不 resume 也不 abort，直接提交「继续」");
  t = fresh();
  await run("first", "S4", t.db, t.m, "SIGKILL");
  await run("submit-without-resume", "S4", t.db, t.m);
  rmSync(t.dir, { recursive: true, force: true });

  console.log("\n== S5 连续崩溃：每次重开都 resume，工具又卡住，再被 SIGKILL（重启上限默认 5）");
  t = fresh();
  await run("first", "S5", t.db, t.m, "SIGKILL");
  for (let i = 1; i <= 6; i++) {
    // again：重开、resume（提交新输入会触发 resume）后工具又卡住 —— 这里用同一个脚本的 recover 角色不能卡住，所以用 loop-probe 之前先手动循环
    const p = spawn("node", [...NODE, "loop-hang", "S5", t.db, t.m], { stdio: "inherit" });
    await sleep(700);
    p.kill("SIGKILL");
    await new Promise((r) => p.on("close", r));
    console.log(`  第 ${i} 次重开后被杀，工具累计执行 ${execs(t.m)} 次`);
  }
  await run("loop-probe", "S5", t.db, t.m);
  rmSync(t.dir, { recursive: true, force: true });
}

if (role === "loop-hang") {
  // S5 的卡住进程：重开、resume，工具（replay never 会产生错误结果，所以这里换成 safe 的卡住工具）
  const Tool = defineTool({
    name: "write_canvas",
    description: "改画布",
    parameters: Type.Object({ n: Type.Number() }),
    replay: "safe",
    execute: async () => { mark("tool-exec loop"); await new Promise(() => {}); },
  });
  const registry = createRegistry();
  registry.install(defineExtension({ name: "canvas", tools: [Tool] }));
  const { models, model } = makeModels([fauxAssistantMessage("收尾完成")]);
  const harness = await Harness.open(await openNodeSqliteStorage(db), { models, registry }, context);
  const root = await harness.root(context, { agent: { model } });
  void root;
  harness.resume();
  await sleep(60_000);
} else if (role === "first" || role === "again" || role === "recover" || role === "abort-then-continue" || role === "submit-without-resume" || role === "loop-probe") {
  await child();
} else {
  await orchestrate();
}
