import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { APIError } from "../src/client.ts";
import { assetUpload } from "../src/commands/index.ts";
import type { Ctx } from "../src/types.ts";

// 本地素材上传：与网页画布走同一条路径——
// 先申请 upload-intent，能直传就直传并登记，否则降级后端中转。
// 断言的是「CLI 实际发了什么请求」，不是代码看起来对不对。

interface SeenRequest {
  method: string;
  url: string;
  authorization: string | undefined;
  contentType: string | undefined;
  headers: http.IncomingHttpHeaders;
  body: Buffer;
}

interface Route {
  status: number;
  body: unknown;
}

/** 假站点：按路径前缀给响应，记录所有请求。 */
function fakeSite(routes: (req: SeenRequest) => Route): Promise<{
  base: string;
  requests: SeenRequest[];
  close: () => Promise<void>;
}> {
  const requests: SeenRequest[] = [];
  return new Promise((resolve) => {
    const server = http.createServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on("data", (c: Buffer) => chunks.push(c));
      req.on("end", () => {
        const seen: SeenRequest = {
          method: req.method || "",
          url: req.url || "",
          authorization: req.headers.authorization,
          contentType: req.headers["content-type"],
          headers: req.headers,
          body: Buffer.concat(chunks),
        };
        requests.push(seen);
        const reply = routes(seen);
        res.writeHead(reply.status, { "content-type": "application/json" });
        res.end(JSON.stringify(reply.body));
      });
    });
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") throw new Error("fake site 没有端口");
      const base = `http://127.0.0.1:${address.port}`;
      resolve({
        base,
        requests,
        close: () => new Promise<void>((done) => server.close(() => done())),
      });
    });
  });
}

function ctxFor(base: string, lines: string[]): Ctx {
  return {
    env: {},
    creds: null,
    credsPath: "/tmp/never",
    base: `${base}/api/v1`,
    baseError: null,
    baseMismatch: null,
    token: "test-token",
    json: true,
    stdout: (line) => lines.push(line),
    stderr: () => {},
    fetchImpl: fetch,
  };
}

function tempFile(content: string, name: string): { dir: string; file: string } {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vc-asset-"));
  const file = path.join(dir, name);
  fs.writeFileSync(file, Buffer.from(content));
  return { dir, file };
}

const INTENT_PROXY = { code: 0, data: { mode: "proxy" } };

test("asset upload：mode=proxy 直接走后端中转，multipart 带 Bearer，输出素材 id", async () => {
  const { dir, file } = tempFile("fake-png-bytes", "shot.png");
  const site = await fakeSite((req) => {
    if (req.url === "/api/v1/assets/upload-intents") return { status: 200, body: INTENT_PROXY };
    return { status: 200, body: { code: 0, data: { id: 12345, kind: "image" } } };
  });
  const lines: string[] = [];
  try {
    await assetUpload(ctxFor(site.base, lines), { positional: [file], flags: {} });
  } finally {
    await site.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }

  assert.equal(site.requests.length, 2);
  const intent = site.requests[0]!;
  assert.equal(intent.url, "/api/v1/assets/upload-intents");
  assert.deepEqual(JSON.parse(intent.body.toString("utf8")), {
    file_name: "shot.png",
    size: 14,
    mime_type: "image/png",
  });
  const upload = site.requests[1]!;
  assert.equal(upload.method, "POST");
  assert.equal(upload.url, "/api/v1/assets");
  assert.equal(upload.authorization, "Bearer test-token");
  assert.match(upload.contentType || "", /^multipart\/form-data; boundary=/);
  assert.match(upload.body.toString("latin1"), /name="file"; filename="shot\.png"/);
  assert.match(
    upload.body.toString("latin1"),
    /Content-Type: image\/png/,
    "file part 必须带真实类型，不能退化成 octet-stream",
  );
  assert.deepEqual(JSON.parse(lines[0] || ""), {
    ok: true,
    data: { id: 12345, kind: "image" },
  });
});

test("asset upload：mode=direct 直传后登记；直传不带登录头，登记带 Bearer", async () => {
  const { dir, file } = tempFile("fake-mp4", "clip.mp4");
  const site = await fakeSite((req) => {
    if (req.url === "/api/v1/assets/upload-intents") {
      return {
        status: 200,
        body: {
          code: 0,
          data: {
            mode: "direct",
            intent_id: 77,
            method: "post",
            url: `${site.base}/bucket`,
            fields: { policy: "abc", key: "u1/202611/x.mp4" },
          },
        },
      };
    }
    if (req.url === "/bucket") return { status: 204, body: {} };
    if (req.url === "/api/v1/assets/upload-intents/77/complete") {
      return { status: 200, body: { code: 0, data: { id: 999, kind: "video" } } };
    }
    return { status: 500, body: { code: -1, msg: "unexpected route" } };
  });
  const lines: string[] = [];
  try {
    await assetUpload(ctxFor(site.base, lines), { positional: [file], flags: {} });
  } finally {
    await site.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }

  assert.equal(site.requests.length, 3);
  const direct = site.requests[1]!;
  assert.equal(direct.method, "POST");
  assert.equal(direct.url, "/bucket");
  assert.equal(direct.authorization, undefined, "直传不能带登录头");
  const directBody = direct.body.toString("latin1");
  assert.ok(
    directBody.indexOf('name="policy"') < directBody.indexOf('name="file"'),
    "fields 必须在 file 之前",
  );
  assert.match(directBody, /fake-mp4/);
  assert.match(
    directBody,
    /Content-Type: video\/mp4/,
    "POST 策略钉了 $Content-Type，part 类型必须与申请的 mime_type 一致",
  );
  const complete = site.requests[2]!;
  assert.equal(complete.url, "/api/v1/assets/upload-intents/77/complete");
  assert.equal(complete.authorization, "Bearer test-token");
  assert.deepEqual(JSON.parse(lines[0] || ""), {
    ok: true,
    data: { id: 999, kind: "video" },
  });
});

test("asset upload：直传失败降级中转，且提示写在 stderr", async () => {
  const { dir, file } = tempFile("fake-png", "fallback.png");
  const site = await fakeSite((req) => {
    if (req.url === "/api/v1/assets/upload-intents") {
      return {
        status: 200,
        body: {
          code: 0,
          data: {
            mode: "direct",
            intent_id: 21,
            method: "put",
            url: `${site.base}/put-bucket`,
            headers: { "x-oss-date": "now" },
          },
        },
      };
    }
    if (req.url === "/put-bucket") return { status: 403, body: {} };
    return { status: 200, body: { code: 0, data: { id: 321, kind: "image" } } };
  });
  const lines: string[] = [];
  const errors: string[] = [];
  const ctx = ctxFor(site.base, lines);
  ctx.stderr = (line) => errors.push(line);
  try {
    await assetUpload(ctx, { positional: [file], flags: {} });
  } finally {
    await site.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }

  assert.equal(site.requests.length, 3);
  assert.equal(site.requests[1]!.method, "PUT");
  assert.equal(site.requests[1]!.headers["x-oss-date"], "now");
  assert.equal(site.requests[2]!.url, "/api/v1/assets");
  assert.ok(errors.some((line) => /直传失败/.test(line)));
  assert.deepEqual(JSON.parse(lines[0] || ""), { ok: true, data: { id: 321, kind: "image" } });
});

test("asset upload：complete 登记失败是真错误，不降级中转", async () => {
  const { dir, file } = tempFile("x", "bad.png");
  const site = await fakeSite((req) => {
    if (req.url === "/api/v1/assets/upload-intents") {
      return {
        status: 200,
        body: {
          code: 0,
          data: { mode: "direct", intent_id: 5, method: "put", url: `${site.base}/bucket` },
        },
      };
    }
    if (req.url === "/bucket") return { status: 200, body: {} };
    if (req.url === "/api/v1/assets/upload-intents/5/complete") {
      return { status: 400, body: { code: 40001, msg: "不支持的文件类型" } };
    }
    return { status: 200, body: { code: 0, data: {} } };
  });
  const lines: string[] = [];
  try {
    await assert.rejects(
      () => assetUpload(ctxFor(site.base, lines), { positional: [file], flags: {} }),
      (err: unknown) => {
        assert.ok(err instanceof APIError);
        assert.equal(err.code, 40001);
        return true;
      },
    );
  } finally {
    await site.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  assert.equal(site.requests.length, 3, "登记失败不能再发中转请求");
});

test("asset upload：申请接口报错时按中转处理", async () => {
  const { dir, file } = tempFile("x", "old-server.png");
  const site = await fakeSite((req) => {
    if (req.url === "/api/v1/assets/upload-intents") {
      return { status: 404, body: { code: 40404, msg: "not found" } };
    }
    return { status: 200, body: { code: 0, data: { id: 11, kind: "image" } } };
  });
  const lines: string[] = [];
  try {
    await assetUpload(ctxFor(site.base, lines), { positional: [file], flags: {} });
  } finally {
    await site.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  assert.equal(site.requests[1]!.url, "/api/v1/assets");
  assert.deepEqual(JSON.parse(lines[0] || ""), { ok: true, data: { id: 11, kind: "image" } });
});

test("asset upload：扩展名不在白名单时不去申请直传，直接中转", async () => {
  const { dir, file } = tempFile("unknown-bytes", "photo.heic");
  const site = await fakeSite(() => {
    return { status: 200, body: { code: 0, data: { id: 42, kind: "image" } } };
  });
  const lines: string[] = [];
  try {
    await assetUpload(ctxFor(site.base, lines), { positional: [file], flags: {} });
  } finally {
    await site.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }

  assert.equal(site.requests.length, 1, "认不出类型就不该申请直传");
  assert.equal(site.requests[0]!.url, "/api/v1/assets");
  assert.deepEqual(JSON.parse(lines[0] || ""), { ok: true, data: { id: 42, kind: "image" } });
});

test("asset upload：没给文件路径是用法错误（status=2），不发请求", async () => {
  const lines: string[] = [];
  await assert.rejects(
    () => assetUpload(ctxFor("http://127.0.0.1:1", lines), { positional: [], flags: {} }),
    (err: unknown) => {
      assert.ok(err instanceof APIError);
      assert.equal(err.status, 2);
      return true;
    },
  );
});

test("asset upload：文件不存在是用法错误（status=2），不发请求", async () => {
  const lines: string[] = [];
  await assert.rejects(
    () =>
      assetUpload(ctxFor("http://127.0.0.1:1", lines), {
        positional: ["/definitely/not/exists.png"],
        flags: {},
      }),
    (err: unknown) => {
      assert.ok(err instanceof APIError);
      assert.equal(err.status, 2);
      return true;
    },
  );
});

const here = path.dirname(fileURLToPath(import.meta.url));
const bin = path.join(here, "..", "dist", "cli.js");

function tempConfig(base: string): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vc-asset-cli-"));
  fs.mkdirSync(path.join(dir, "video-canvas"), { recursive: true });
  fs.writeFileSync(
    path.join(dir, "video-canvas", "credentials.json"),
    JSON.stringify({ base: `${base}/api/v1`, token: "cli-token", role: "user" }),
  );
  return dir;
}

test("vc asset upload：顶层分发可用，--json 只输出一行素材数据", async () => {
  const { dir, file } = tempFile("fake-mp4", "e2e.mp4");
  const site = await fakeSite((req) => {
    if (req.url === "/api/v1/assets/upload-intents") return { status: 200, body: INTENT_PROXY };
    return { status: 200, body: { code: 0, data: { id: 999, kind: "video" } } };
  });

  try {
    const config = tempConfig(site.base);
    const result = await new Promise<{ code: number | null; stdout: string; stderr: string }>(
      (resolve) => {
        const child = spawn(process.execPath, [bin, "asset", "upload", file, "--json"], {
          env: { ...process.env, XDG_CONFIG_HOME: config, VC_NO_UPDATE_CHECK: "1" },
        });
        let stdout = "";
        let stderr = "";
        child.stdout.on("data", (c: Buffer) => (stdout += c.toString()));
        child.stderr.on("data", (c: Buffer) => (stderr += c.toString()));
        child.on("close", (code) => resolve({ code, stdout, stderr }));
      },
    );

    assert.equal(result.code, 0, `stderr: ${result.stderr}`);
    assert.equal(site.requests[1]!.authorization, "Bearer cli-token");
    const lines = result.stdout.trim().split("\n");
    assert.equal(lines.length, 1);
    assert.deepEqual(JSON.parse(lines[0] || ""), {
      ok: true,
      data: { id: 999, kind: "video" },
    });
  } finally {
    await site.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
