import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { deriveComposeAssets, parseArgs } from "../src/commands/index.ts";
import type { Graph } from "../src/types.ts";

// 生成 / 合成 / 短剧三类命令的请求体与输出：用真实 HTTP 的假站点，
// 断言的是「CLI 实际发了什么」，而不是「代码看起来像是对的」。

const here = path.dirname(fileURLToPath(import.meta.url));
const bin = path.join(here, "..", "dist", "cli.js");

/** 测试里断言的 JSON 体：结构随用例变，不在这里逐一建模。 */
type Json = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

const MODELS = [
  {
    key: "agnes-image",
    kind: "image",
    label: "Agnes 图片",
    hint: "通用出图",
    capabilities: {
      ops: ["text2image"],
      // 接口里 params 是「名字 -> 字段」的对象，键序就是画布参数面板的顺序
      params: { ratio: { type: "enum", options: ["1:1", "16:9"], label: "画幅" } },
    },
  },
  {
    key: "agnes-video",
    kind: "video",
    label: "Agnes 视频",
    hint: "",
    capabilities: {
      ops: ["text2video"],
      params: { duration: { type: "number", min: 1, max: 10, label: "时长" } },
    },
  },
];

interface FakeSite {
  posts: Json[];
  headers: Json[];
  polls: number;
  task: Json | null;
  compose: Json | null;
  base: string;
  close(): Promise<void>;
}

async function fakeSite(): Promise<FakeSite> {
  const state: FakeSite = {
    posts: [],
    headers: [],
    polls: 0,
    task: null,
    compose: null,
    base: "",
    close: async () => {},
  };
  const send = (res: http.ServerResponse, body: unknown, status = 200): void => {
    res.writeHead(status, { "content-type": "application/json" });
    res.end(JSON.stringify(body));
  };
  const readBody = (req: http.IncomingMessage): Promise<Json> =>
    new Promise((resolve) => {
      let raw = "";
      req.on("data", (c: Buffer) => (raw += c.toString()));
      req.on("end", () => resolve(raw ? (JSON.parse(raw) as Json) : {}));
    });

  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", "http://x");
    const p = url.pathname;

    if (req.method === "GET" && p === "/api/v1/models") {
      const kind = url.searchParams.get("kind");
      return send(res, { code: 0, data: kind ? MODELS.filter((m) => m.kind === kind) : MODELS });
    }
    if (req.method === "GET" && p === "/api/v1/credits") {
      return send(res, { code: 0, data: { balance: 120, frozen: 20, available: 100 } });
    }
    if (req.method === "POST" && p === "/api/v1/generation-tasks") {
      const body = await readBody(req);
      state.posts.push(body);
      state.headers.push({ idempotency: req.headers["idempotency-key"] });
      const count = ((body.node_ids as unknown[]) || [body.node_id]).filter(Boolean).length || 1;
      const items = Array.from({ length: count }, (_, i) => ({
        node_id: body.node_id || "",
        task: {
          id: 500 + i,
          task_ref: `t${500 + i}`,
          status: "queued",
          kind: body.kind,
          model_id: body.model_id,
          credits: 5,
          progress: 0,
        },
      }));
      return send(res, { code: 0, data: { items } }, 202);
    }
    if (req.method === "GET" && /^\/api\/v1\/generation-tasks\/\d+$/.test(p)) {
      state.polls++;
      return send(res, { code: 0, data: state.task });
    }
    if (req.method === "POST" && p === "/api/v1/canvas") {
      const body = await readBody(req);
      state.posts.push(body);
      return send(res, {
        code: 0,
        data: { id: "c_new", title: body.title, revision: 1, payload_json: body.payload_json },
      });
    }
    if (req.method === "POST" && p === "/api/v1/canvas/import") {
      const body = await readBody(req);
      state.posts.push(body);
      return send(res, {
        code: 0,
        data: {
          canvas: {
            id: "c_imported",
            title: body.document?.canvas?.title || "导入的画布",
            revision: 1,
          },
          report: { assets: { copied: 1, total: 1, missing: [] }, edges_dropped: 0 },
        },
      });
    }
    if (req.method === "GET" && /^\/api\/v1\/canvas\/[^/]+$/.test(p)) {
      return send(res, {
        code: 0,
        data: { id: "c1", title: "成片测试", revision: 2, payload_json: GRAPH },
      });
    }
    if (req.method === "POST" && /^\/api\/v1\/canvas\/[^/]+\/compose$/.test(p)) {
      state.posts.push(await readBody(req));
      return send(res, {
        code: 0,
        data: {
          id: "job1",
          canvas_id: "c1",
          node_id: "c_node",
          status: "queued",
          fingerprint: "f",
          duration_ms: 0,
          segment_count: 2,
        },
      });
    }
    if (req.method === "GET" && /^\/api\/v1\/canvas\/[^/]+\/compose\/[^/]+$/.test(p)) {
      return send(res, { code: 0, data: state.compose });
    }
    if (req.method === "GET" && p === "/api/v1/dramas") {
      return send(res, {
        code: 0,
        data: {
          list: [
            {
              id: "d1",
              title: "雪夜归途",
              status: "planning",
              phase: "running",
              episode_count: 12,
              locked_count: 3,
              updated_at: "2026-11-23T10:00:00Z",
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
        },
      });
    }
    if (req.method === "GET" && p === "/api/v1/dramas/d1") {
      return send(res, {
        code: 0,
        data: {
          id: "d1",
          title: "雪夜归途",
          status: "planning",
          phase: "done",
          canvas_id: "c9",
          source_chars: 5200,
          units_total: 40,
          unit_kind: "scene",
          target_episodes: 12,
          episode_seconds: 60,
          ratio: "9:16",
          // 12 集：默认只列前 10 集，验证不会把整季刷满终端
          episodes: Array.from({ length: 12 }, (_, i) => ({
            no: i + 1,
            title: `第${i + 1}集`,
            unit_from: i + 1,
            unit_to: i + 1,
            script_status: "draft",
          })),
        },
      });
    }
    if (req.method === "GET" && p === "/api/v1/dramas/d1/units") {
      // 真实接口把列表包在 items 里，不是裸数组；按裸数组解析会得到「(没有单元)」
      return send(res, {
        code: 0,
        data: {
          items: [
            { no: 1, title: "开场", chars: 420 },
            { no: 2, title: "冲突", chars: 380 },
          ],
        },
      });
    }
    if (req.method === "GET" && p === "/api/v1/dramas/d1/units/1") {
      return send(res, {
        code: 0,
        data: { no: 1, title: "开场", text: "雪夜里，她推开了那扇门。" },
      });
    }
    if (req.method === "GET" && p === "/api/v1/dramas/d1/jobs") {
      return send(res, {
        code: 0,
        data: {
          items: [
            {
              id: "j1",
              step: "plan",
              status: "running",
              progress: 40,
              episodes: Array.from({ length: 96 }, (_, i) => i + 1),
              lane: "plan",
              credits: 3,
            },
          ],
        },
      });
    }
    if (req.method === "POST" && p === "/api/v1/dramas/d1/jobs/j1/cancel") {
      return send(res, {
        code: 0,
        data: {
          id: "j1",
          step: "plan",
          status: "canceled",
          progress: 40,
          episodes: [1],
          credits: 3,
        },
      });
    }
    return send(res, { code: 404, msg: "not found" }, 404);
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const addr = server.address();
  const port = typeof addr === "object" && addr ? addr.port : 0;
  state.base = `http://127.0.0.1:${port}`;
  state.close = () =>
    new Promise<void>((resolve) => {
      server.closeAllConnections?.();
      server.close(() => resolve());
    });
  return state;
}

// 成片节点连了三个上游：两个视频（v1 在 v2 上面）与一个图片；composeOrder 把 v2 提到前面，
// v1 走 activeOutputId → outputs 里那一版（111），而不是老字段 assetId（101）。
const GRAPH: Graph = {
  nodes: [
    {
      id: "v1",
      type: "canvas",
      position: { x: 0, y: 100 },
      data: {
        kind: "video",
        label: "镜头1",
        assetId: "101",
        activeOutputId: "o1",
        outputs: [{ id: "o1", assetId: "111" }],
      },
    },
    {
      id: "v2",
      type: "canvas",
      position: { x: 0, y: 200 },
      data: { kind: "video", label: "镜头2", assetId: "202" },
    },
    {
      id: "img",
      type: "canvas",
      position: { x: 0, y: 50 },
      data: { kind: "image", label: "首图", assetId: "900" },
    },
    {
      id: "c_node",
      type: "canvas",
      position: { x: 300, y: 100 },
      data: { kind: "compose", label: "成片", composeOrder: ["v2"] },
    },
  ],
  edges: [
    { id: "e1", source: "v1", target: "c_node" },
    { id: "e2", source: "v2", target: "c_node" },
    { id: "e3", source: "img", target: "c_node" },
  ],
};

function tempConfig(base: string): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vc-gen-"));
  fs.mkdirSync(path.join(dir, "video-canvas"), { recursive: true });
  fs.writeFileSync(
    path.join(dir, "video-canvas", "credentials.json"),
    JSON.stringify({ base: `${base}/api/v1`, token: "t", role: "user" }),
  );
  return dir;
}

interface RunResult {
  code: number | null;
  stdout: string;
  stderr: string;
}

/** 一律异步：假服务跑在本进程里，spawnSync 会把事件循环卡住，请求永远等不到响应。 */
function run(args: string[], env: Record<string, string>): Promise<RunResult> {
  return runAsync(args, env);
}

/** 要等终态的命令必须异步跑：假服务在本进程里，spawnSync 会把事件循环卡死。 */
function runAsync(args: string[], env: Record<string, string>): Promise<RunResult> {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [bin, ...args], {
      env: { ...process.env, VC_NO_UPDATE_CHECK: "1", ...env },
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (c: Buffer) => (stdout += c.toString()));
    child.stderr.on("data", (c: Buffer) => (stderr += c.toString()));
    child.on("close", (code) => resolve({ code, stdout, stderr }));
  });
}

test("model list：按种类过滤并打印表头", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const all = await run(["model", "list"], env);
    assert.equal(all.code, 0);
    assert.match(all.stdout, /^KEY\tKIND\tLABEL/m);
    assert.match(all.stdout, /agnes-image/);
    assert.match(all.stdout, /agnes-video/);

    const onlyVideo = await run(["model", "list", "--kind", "video", "--json"], env);
    const body = JSON.parse(onlyVideo.stdout) as { ok: boolean; data: Array<{ key: string }> };
    assert.equal(body.ok, true);
    assert.deepEqual(
      body.data.map((m) => m.key),
      ["agnes-video"],
    );
  } finally {
    await site.close();
  }
});

test("model show：打印参数可选值与范围，找不到时给出相近项", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const ok = await run(["model", "show", "agnes-image"], env);
    assert.equal(ok.code, 0);
    assert.match(ok.stdout, /参数 ratio/);
    assert.match(ok.stdout, /1:1 \/ 16:9/);

    const miss = await run(["model", "show", "agnes-imag"], env);
    assert.equal(miss.code, 1);
    assert.match(miss.stderr, /相近的有：agnes-image/);
  } finally {
    await site.close();
  }
});

test("credits：三行数值一眼看清", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, stdout } = await run(["credits"], env);
    assert.equal(code, 0);
    assert.match(stdout, /余额 120\s+冻结 20\s+可用 100/);
  } finally {
    await site.close();
  }
});

test("generate：prompt / param / 参考素材合成 input，count 落成 node_ids，带幂等键", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, stdout } = await run(
      [
        "generate",
        "video",
        "--model",
        "agnes-video",
        "--canvas",
        "c1",
        "--node",
        "n1",
        "--prompt",
        "海边日出",
        "--param",
        "duration=5",
        "--param",
        "motion=strong",
        "--ref-image",
        "101,102",
        "--count",
        "2",
      ],
      env,
    );
    assert.equal(code, 0, stdout);
    assert.equal(site.posts.length, 1);
    const body = site.posts[0] as Json;
    assert.equal(body.kind, "video");
    assert.equal(body.model_id, "agnes-video");
    assert.equal(body.canvas_id, "c1");
    assert.deepEqual(body.node_ids, ["n1", "n1"]);
    assert.equal(body.input.prompt, "海边日出");
    assert.equal(body.input.duration, 5, "数字参数要按 JSON 解释成数字");
    assert.equal(body.input.motion, "strong", "解析不出来的按字符串");
    assert.deepEqual(body.input.images, ["101", "102"]);
    assert.ok(site.headers[0]?.idempotency, "必须带 Idempotency-Key");
    assert.match(stdout, /任务 t500/);
    assert.match(stdout, /冻结 5 积分/);
  } finally {
    await site.close();
  }
});

test("generate：参数不足与半截参数给用法错误，且不发请求", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const noKind = await run(["generate", "--model", "agnes-video"], env);
    assert.equal(noKind.code, 2);
    assert.match(noKind.stderr, /第一项要写生成种类/);

    const noInput = await run(
      ["generate", "video", "--model", "agnes-video", "--canvas", "c1", "--node", "n1"],
      env,
    );
    assert.equal(noInput.code, 2);
    assert.match(noInput.stderr, /至少要给一个生成参数/);

    const half = await run(["generate", "video", "--model", "agnes-video", "--canvas", "c1"], env);
    assert.equal(half.code, 2);
    assert.match(half.stderr, /--canvas 与 --node 要一起给/);

    const badRef = await run(
      ["generate", "image", "--model", "agnes-image", "--prompt", "x", "--ref-image", "abc"],
      env,
    );
    assert.equal(badRef.code, 2);
    assert.match(badRef.stderr, /要素材 id/);
    assert.equal(site.posts.length, 0);
  } finally {
    await site.close();
  }
});

test("generate --wait：成功时打印产物地址，退出码 0", async () => {
  const site = await fakeSite();
  try {
    site.task = {
      id: 500,
      task_ref: "t500",
      status: "succeeded",
      kind: "video",
      model_id: "agnes-video",
      credits: 5,
      progress: 100,
      outputs: [{ url: "https://cdn.test/a.mp4" }],
    };
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, stdout } = await runAsync(
      ["generate", "video", "--model", "agnes-video", "--prompt", "x", "--wait", "--interval", "1"],
      env,
    );
    assert.equal(code, 0, stdout);
    assert.match(stdout, /https:\/\/cdn\.test\/a\.mp4/);
    assert.ok(site.polls >= 1);
  } finally {
    await site.close();
  }
});

test("generate --wait：任务失败时 ok:false 且退出码 1", async () => {
  const site = await fakeSite();
  try {
    site.task = {
      id: 500,
      task_ref: "t500",
      status: "failed",
      kind: "video",
      model_id: "agnes-video",
      credits: 5,
      progress: 100,
      error_code: 30001,
      error_message: "上游超时",
    };
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, stdout } = await runAsync(
      [
        "generate",
        "video",
        "--model",
        "agnes-video",
        "--prompt",
        "x",
        "--wait",
        "--interval",
        "1",
        "--json",
      ],
      env,
    );
    assert.equal(code, 1);
    const body = JSON.parse(stdout) as { ok: boolean; error: { message: string } };
    assert.equal(body.ok, false);
    assert.match(body.error.message, /上游超时/);
  } finally {
    await site.close();
  }
});

test("canvas compose*：旧服务端没有协作成片能力时拒绝提交", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const calls = [
      [
        "canvas",
        "compose",
        "c1",
        "--nodes",
        "v2,v1",
        "--epoch",
        "1",
        "--revision",
        "2",
        "--op-id",
        "render",
      ],
    ];
    for (const argv of calls) {
      const { code, stderr } = await run(argv, env);
      assert.equal(code, 1, argv.join(" "));
      assert.match(stderr, /当前服务端暂不支持成片合成/);
    }
    assert.equal(site.posts.length, 0, "不能发出任何合成请求");
  } finally {
    await site.close();
  }
});

test("deriveComposeAssets：按 composeOrder 先、其余按位置，v1 取 activeOutputId 那一版", () => {
  // 命令入口暂时关闭，推导规则仍要有测试守着，后端就绪后直接恢复
  const { rows, missing } = deriveComposeAssets(GRAPH, "c_node");
  assert.deepEqual(
    rows.map((row) => row.assetId),
    ["202", "111"],
  );
  assert.deepEqual(missing, []);
  assert.throws(() => deriveComposeAssets(GRAPH, "v1"), /不是成片节点/);
});

test("--help 列出成片合成命令", async () => {
  const { stdout } = await run(["--help"], { XDG_CONFIG_HOME: tempConfig("http://127.0.0.1:1") });
  assert.match(stdout, /canvas compose <code>/);
});

test("drama：列表、详情、单元、任务四条都是只读", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };

    const list = await run(["drama", "list"], env);
    assert.equal(list.code, 0);
    assert.match(list.stdout, /雪夜归途/);
    assert.match(list.stdout, /共 1 部/);

    const detail = await run(["drama", "get", "d1"], env);
    assert.equal(detail.code, 0);
    assert.match(detail.stdout, /单元：40 个/);
    assert.match(detail.stdout, /第 1 集/);
    // 12 集的短剧默认只列前 10 集（实测 96 集项目会打 175 行）
    assert.match(detail.stdout, /只列前 10/);
    assert.doesNotMatch(detail.stdout, /第 12 集  /);
    const allEpisodes = await run(["drama", "get", "d1", "--episodes", "0"], env);
    assert.match(allEpisodes.stdout, /第 12 集/);

    const units = await run(["drama", "units", "d1"], env);
    assert.equal(units.code, 0);
    assert.match(units.stdout, /NO\tTITLE\tCHARS/);
    assert.match(units.stdout, /开场/);

    const unit = await run(["drama", "unit", "d1", "1"], env);
    assert.equal(unit.code, 0);
    assert.match(unit.stdout, /雪夜里，她推开了那扇门/);

    const jobs = await run(["drama", "jobs", "d1", "--active"], env);
    assert.equal(jobs.code, 0);
    assert.match(jobs.stdout, /JOB\tSTEP\tSTATUS/);
    // 96 个集号要压缩成区间，否则一行表格被挤成两屏
    assert.match(jobs.stdout, /1,2,3…95,96（96 集）/);

    const cancel = await run(["drama", "job-cancel", "d1", "--job", "j1"], env);
    assert.equal(cancel.code, 0);
    assert.match(cancel.stdout, /canceled/);
  } finally {
    await site.close();
  }
});

test("parseArgs：多值 flag 收成数组，单值 flag 仍然后者生效", () => {
  const { flags } = parseArgs([
    "--param",
    "a=1",
    "--param",
    "b=2",
    "--label",
    "第一次",
    "--label",
    "第二次",
  ]);
  assert.deepEqual(flags.param, ["a=1", "b=2"]);
  assert.equal(flags.label, "第二次");
});

// ---------------------------------------------------------------------------
// 新建项目（画布）必须由人确认：脚本不该能在无人知情时堆出一堆项目
// ---------------------------------------------------------------------------

/** 用伪终端跑一次，模拟真人敲 yes / no（借 python3 造 pty，与 node-ops 同一套办法）。 */
function runInPty(
  args: string[],
  env: Record<string, string>,
  input: string,
): Promise<{ code: number | null; out: string }> {
  const helper = path.join(here, "helpers", "pty_run.py");
  return new Promise((resolve) => {
    const child = spawn("python3", [helper, process.execPath, bin, ...args], {
      env: { ...process.env, VC_NO_UPDATE_CHECK: "1", PTY_INPUT: input, ...env },
    });
    let out = "";
    child.stdout.on("data", (c: Buffer) => (out += c.toString()));
    child.stderr.on("data", (c: Buffer) => (out += c.toString()));
    child.on("close", (code) => resolve({ code, out }));
  });
}

const hasPty = spawnSync("python3", ["-c", "import pty"], { encoding: "utf8" }).status === 0;

test("canvas create：非交互终端直接拒绝，且不发出建画布请求", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, stderr } = await run(["canvas", "create", "--title", "不该建出来"], env);
    assert.equal(code, 2);
    assert.match(stderr, /需要交互确认/);
    assert.equal(site.posts.length, 0, "拒绝时不能建画布");
  } finally {
    await site.close();
  }
});

test("canvas create：--json 一律拒绝（没有脚本旁路）", async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, stdout } = await run(["canvas", "create", "--title", "x", "--json"], env);
    assert.equal(code, 2);
    assert.match(stdout, /不能与 --json 一起用/);
    assert.equal(site.posts.length, 0);
  } finally {
    await site.close();
  }
});

test("canvas create：终端里输入 yes 才建，并把标题传给服务端", { skip: !hasPty }, async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, out } = await runInPty(
      ["canvas", "create", "--title", "确认过的画布"],
      env,
      "yes",
    );
    assert.equal(code, 0, out);
    assert.match(out, /将新建画布/);
    assert.match(out, /已创建：确认过的画布/);
    assert.equal(site.posts.length, 1);
    assert.equal((site.posts[0] as Json).title, "确认过的画布");
  } finally {
    await site.close();
  }
});

test("canvas create：输入 yes 以外的东西算取消，退出码 4 且不建", { skip: !hasPty }, async () => {
  const site = await fakeSite();
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const { code, out } = await runInPty(["canvas", "create", "--title", "不该建出来"], env, "n");
    assert.equal(code, 4);
    assert.match(out, /已取消，没有新建任何画布/);
    assert.equal(site.posts.length, 0);
  } finally {
    await site.close();
  }
});

test("canvas import：确认后按导出文档新建，并把文档原样传给服务端", { skip: !hasPty }, async () => {
  const site = await fakeSite();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vc-import-"));
  const file = path.join(dir, "export.json");
  fs.writeFileSync(
    file,
    JSON.stringify({
      format: "video-canvas/canvas",
      canvas: { title: "被导入的画布", nodes: [{ id: "n1" }], edges: [] },
      assets: { a1: { url: "x" } },
    }),
  );
  try {
    const env = { XDG_CONFIG_HOME: tempConfig(site.base) };
    const no = await run(["canvas", "import", file], env);
    assert.equal(no.code, 2, "非交互终端应拒绝");
    assert.equal(site.posts.length, 0);

    const { code, out } = await runInPty(["canvas", "import", file], env, "yes");
    assert.equal(code, 0, out);
    assert.match(out, /被导入的画布/);
    assert.equal(site.posts.length, 1);
    assert.equal((site.posts[0] as Json).document.canvas.title, "被导入的画布");
  } finally {
    await site.close();
  }
});
