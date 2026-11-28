import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

import { ok, runCli, startSite, tempConfig } from "./helpers/site.ts";

const hasFFmpeg = spawnSync("ffmpeg", ["-version"], { stdio: "ignore" }).status === 0;

test("人声分离固定源音频和扣费上限，并复用已受理任务", async (context) => {
  let confirmed = 0;
  const input = { audio_operation: "vocal_separation", audios: ["9007199254740993"] };
  const site = await startSite(({ path, body, headers }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canGenerate: true,
        generationBudgetLimit: true,
        canvas: {
          id: "c1",
          revision: 7 + confirmed,
          payload_json: {
            nodes: [{ id: "audio", data: { kind: "audio", assetId: "9007199254740993" } }],
          },
        },
      });
    if (path.endsWith("/me")) return ok({ id: 10 });
    if (path.endsWith("/collaboration/ops")) {
      confirmed++;
      assert.deepEqual(body.changes[0].fields["data.generationInput"].input, input);
      return ok({ epoch: 3, revision: 8, opId: body.opId });
    }
    if (path.endsWith("/generation-tasks")) {
      assert.equal(headers["idempotency-key"], "split-01");
      assert.deepEqual(body.collaboration, { epoch: 3, revision: 8, max_credits: 4 });
      assert.deepEqual(body.input, input);
      return ok({ items: [{ task: { id: 43, status: "pending" } }] });
    }
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "test-token" });
  context.after(() => fs.rmSync(config, { recursive: true, force: true }));
  const args = [
    "canvas",
    "media",
    "separate-voice",
    "c1",
    "audio",
    "--source-asset",
    "9007199254740993",
    "--epoch",
    "3",
    "--revision",
    "7",
    "--model",
    "split",
    "--max-credits",
    "4",
    "--idempotency-key",
    "split-01",
    "--json",
  ];
  for (const attempt of [1, 2]) {
    const result = await runCli(args, { XDG_CONFIG_HOME: config });
    assert.equal(result.code, 0, `attempt ${attempt}: ${result.stdout} ${result.stderr}`);
    assert.equal(JSON.parse(result.stdout).data.items[0].task.id, 43);
  }
  assert.equal(confirmed, 1);
});

test(
  "扩图底片保留原图像素和透明边缘，并通过协作积分流程生成",
  { skip: !hasFFmpeg },
  async (context) => {
    const root = tempConfig();
    context.after(() => fs.rmSync(root, { recursive: true, force: true }));
    const source = path.join(root, "source.png");
    const made = spawnSync("ffmpeg", [
      "-v",
      "error",
      "-f",
      "lavfi",
      "-i",
      "color=white:s=4x4",
      "-frames:v",
      "1",
      source,
    ]);
    assert.equal(made.status, 0, made.stderr.toString());
    let confirmed = 0;
    const site = await startSite(({ path, body, headers }) => {
      if (path.endsWith("/collaboration"))
        return ok({
          epoch: 3,
          role: "editor",
          canGenerate: true,
          generationBudgetLimit: true,
          canvas: {
            id: "c1",
            revision: 7 + confirmed,
            payload_json: { nodes: [{ id: "shot", data: { kind: "image", assetId: "50" } }] },
          },
        });
      if (path.endsWith("/media/50"))
        return { body: fs.readFileSync(source), contentType: "image/png" };
      if (path.endsWith("/models"))
        return ok([
          {
            key: "img",
            kind: "image",
            capabilities: { ops: ["i2i"], refs: { image: { on: true } } },
          },
        ]);
      if (path.endsWith("/me")) return ok({ id: 10 });
      if (path.endsWith("/collaboration/ops")) {
        confirmed++;
        assert.equal(body.requireBaseRevision, true);
        assert.deepEqual(body.changes[0].fields["data.generationInput"].input, {
          op: "i2i",
          prompt: "保持原图内容不变，自然补全图片中的透明区域。把背景向左延伸",
          images: ["50"],
        });
        return ok({ epoch: 3, revision: 8, opId: body.opId });
      }
      if (path.endsWith("/generation-tasks")) {
        assert.equal(headers["idempotency-key"], "extend-01");
        assert.deepEqual(body.collaboration, { epoch: 3, revision: 8, max_credits: 4 });
        return ok({ items: [{ task: { id: 42, status: "pending" } }] });
      }
    });
    context.after(() => site.close());
    const config = tempConfig({ base: site.base, token: "t" });
    context.after(() => fs.rmSync(config, { recursive: true, force: true }));
    const env = { XDG_CONFIG_HOME: config };
    const output = path.join(root, "expanded.png");
    const prep = await runCli(
      [
        "canvas",
        "media",
        "outpaint",
        "c1",
        "shot",
        "--epoch",
        "3",
        "--revision",
        "7",
        "--left",
        "2",
        "--right",
        "2",
        "-o",
        output,
        "--json",
      ],
      env,
    );
    assert.equal(prep.code, 0, `${prep.stdout} ${prep.stderr}`);
    const rendered = spawnSync("ffmpeg", [
      "-v",
      "error",
      "-i",
      output,
      "-f",
      "rawvideo",
      "-pix_fmt",
      "rgba",
      "pipe:1",
    ]);
    assert.equal(rendered.status, 0, rendered.stderr.toString());
    assert.equal(rendered.stdout.length, 8 * 4 * 4);
    assert.equal(rendered.stdout[3], 0);
    const original = spawnSync("ffmpeg", [
      "-v",
      "error",
      "-i",
      source,
      "-f",
      "rawvideo",
      "-pix_fmt",
      "rgba",
      "pipe:1",
    ]);
    assert.equal(original.status, 0, original.stderr.toString());
    for (let row = 0; row < 4; row++) {
      const start = row * 8 * 4;
      assert.deepEqual(
        rendered.stdout.subarray(start + 8, start + 24),
        original.stdout.subarray(row * 16, (row + 1) * 16),
      );
    }
    const args = [
      "canvas",
      "media",
      "outpaint-submit",
      "c1",
      "shot",
      "--source-asset",
      "50",
      "--epoch",
      "3",
      "--revision",
      "7",
      "--model",
      "img",
      "--prompt",
      "把背景向左延伸",
      "--max-credits",
      "4",
      "--idempotency-key",
      "extend-01",
      "--json",
    ];
    for (const attempt of [1, 2]) {
      const result = await runCli(args, env);
      assert.equal(result.code, 0, `attempt ${attempt}: ${result.stdout} ${result.stderr}`);
      assert.equal(JSON.parse(result.stdout).data.items[0].task.id, 42);
    }
    assert.equal(confirmed, 1);
  },
);

test("暂存携带原始素材版本并可按同一操作编号确认挂载", async (context) => {
  const site = await startSite(({ path, method, query, raw, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: { id: "c1", revision: 19 },
        capabilities: { actions: ["media-stage", "media-attach"] },
      });
    if (path.endsWith("/media/drafts/processed") && method === "POST") {
      assert.equal(query.get("sourceNode"), "shot");
      assert.equal(query.get("sourceAsset"), "9007199254740993");
      assert.equal(query.get("revision"), "7");
      assert.equal(query.get("epoch"), "3");
      assert.match(raw, /filename="output\.png"/);
      return ok({ draftId: "processed" });
    }
    if (path.endsWith("/media/drafts/processed/attach") && method === "POST") {
      assert.deepEqual(body, { epoch: 3 });
      return ok({
        opId: "processed",
        revision: 20,
        epoch: 3,
        changes: [{ id: "derived" }, { id: "source-link" }],
      });
    }
  });
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  context.after(() => site.close());
  const file = path.join(root, "output.png");
  fs.writeFileSync(file, "processed file");
  const env = { XDG_CONFIG_HOME: root };
  const staged = await runCli(
    [
      "canvas",
      "media",
      "stage",
      "c1",
      "shot",
      "--source-asset",
      "9007199254740993",
      "--epoch",
      "3",
      "--revision",
      "7",
      "--op-id",
      "processed",
      "--file",
      file,
      "--json",
    ],
    env,
  );
  assert.equal(staged.code, 0, `${staged.stdout} ${staged.stderr}`);
  for (const attempt of [1, 2]) {
    const attached = await runCli(
      ["canvas", "media", "attach", "c1", "--epoch", "3", "--op-id", "processed", "--json"],
      env,
    );
    assert.equal(attached.code, 0, `attempt ${attempt}: ${attached.stdout} ${attached.stderr}`);
    assert.match(attached.stdout, /"revision":20/);
  }
});

test("在授权图片上渲染笔画，同时保留未标注像素", { skip: !hasFFmpeg }, async (context) => {
  const root = tempConfig();
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const source = path.join(root, "source.png");
  const made = spawnSync("ffmpeg", [
    "-v",
    "error",
    "-f",
    "lavfi",
    "-i",
    "color=black:s=32x32",
    "-frames:v",
    "1",
    source,
  ]);
  assert.equal(made.status, 0, made.stderr.toString());
  const image = fs.readFileSync(source);
  let revision = 7;
  let changeSource = false;
  const site = await startSite(({ path }) => {
    if (path.endsWith("/media/50")) {
      if (changeSource) revision++;
      return { body: image, contentType: "image/png" };
    }
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision,
          payload_json: { nodes: [{ id: "shot", data: { kind: "image", assetId: "50" } }] },
        },
      });
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(config, { recursive: true, force: true }));
  const dest = path.join(root, "annotated.png");
  const input = path.join(root, "strokes.json");
  fs.writeFileSync(
    input,
    JSON.stringify({
      strokes: [
        {
          color: "#FF0000",
          width: 3,
          points: [
            [0.25, 0.5],
            [0.75, 0.5],
          ],
        },
      ],
    }),
  );
  const args = [
    "canvas",
    "media",
    "annotate",
    "c1",
    "shot",
    "--epoch",
    "3",
    "--revision",
    "7",
    "--input",
    input,
    "-o",
    dest,
    "--json",
  ];
  const result = await runCli(args, { XDG_CONFIG_HOME: config });
  assert.equal(result.code, 0, result.stdout);
  assert.equal(JSON.parse(result.stdout).data.source.assetId, "50");
  const decoded = spawnSync("ffmpeg", [
    "-v",
    "error",
    "-i",
    dest,
    "-f",
    "rawvideo",
    "-pix_fmt",
    "rgb24",
    "pipe:1",
  ]);
  assert.equal(decoded.status, 0, decoded.stderr.toString());
  assert.deepEqual([...decoded.stdout.subarray(0, 3)], [0, 0, 0]);
  const center = (16 * 32 + 16) * 3;
  assert.deepEqual([...decoded.stdout.subarray(center, center + 3)], [255, 0, 0]);
  const exists = await runCli(args, { XDG_CONFIG_HOME: config });
  assert.equal(exists.code, 1, exists.stdout);
  const invalidFile = path.join(root, "invalid.json");
  fs.writeFileSync(invalidFile, '{"strokes":[{"color":"red", "width":2, "points":[[0,0]]}]}');
  const invalid = await runCli(
    args.map((entry) => (entry === input ? invalidFile : entry)),
    {
      XDG_CONFIG_HOME: config,
    },
  );
  assert.equal(invalid.code, 2, invalid.stdout);
  assert.equal(site.requests.filter(({ path }) => path.endsWith("/media/50")).length, 2);
  changeSource = true;
  const stale = path.join(root, "stale.png");
  const raced = await runCli(
    args.map((entry) => (entry === dest ? stale : entry)),
    {
      XDG_CONFIG_HOME: config,
    },
  );
  assert.equal(JSON.parse(raced.stdout).error.reason, "conflict");
  assert.equal(fs.existsSync(stale), false);
  assert.ok(!fs.readdirSync(root).some((entry) => entry.startsWith(".vc-media-")));
});

test(
  "按自定义切线拆分授权图片，保留奇数像素并拒绝覆盖输出目录",
  { skip: !hasFFmpeg },
  async (context) => {
    const root = tempConfig();
    context.after(() => fs.rmSync(root, { recursive: true, force: true }));
    const source = path.join(root, "source.png");
    const made = spawnSync("ffmpeg", [
      "-v",
      "error",
      "-f",
      "lavfi",
      "-i",
      "testsrc=s=65x47",
      "-frames:v",
      "1",
      source,
    ]);
    assert.equal(made.status, 0, made.stderr.toString());
    const image = fs.readFileSync(source);
    let revision = 7;
    let changeSource = false;
    const site = await startSite(({ path }) => {
      if (path.endsWith("/media/50")) {
        if (changeSource) revision++;
        return { body: image, contentType: "image/png" };
      }
      if (path.endsWith("/collaboration"))
        return ok({
          epoch: 3,
          role: "editor",
          canvas: {
            id: "c1",
            revision,
            payload_json: { nodes: [{ id: "shot", data: { kind: "image", assetId: "50" } }] },
          },
        });
    });
    context.after(() => site.close());
    const config = tempConfig({ base: site.base, token: "t" });
    context.after(() => fs.rmSync(config, { recursive: true, force: true }));
    const dest = path.join(root, "tiles");
    const cuts = path.join(root, "cuts.json");
    fs.writeFileSync(cuts, '{"xs":[0.3],"ys":[0.75]}');
    const args = [
      "canvas",
      "media",
      "grid",
      "c1",
      "shot",
      "--epoch",
      "3",
      "--revision",
      "7",
      "--input",
      cuts,
      "-o",
      dest,
      "--json",
    ];
    const result = await runCli(args, { XDG_CONFIG_HOME: config });
    assert.equal(result.code, 0, result.stdout);
    const data = JSON.parse(result.stdout).data;
    assert.deepEqual(
      data.tiles.map(({ width, height }: { width: number; height: number }) => [width, height]),
      [
        [20, 35],
        [45, 35],
        [20, 12],
        [45, 12],
      ],
    );
    for (const tile of data.tiles) {
      const bytes = fs.readFileSync(tile.path);
      assert.equal(bytes.subarray(1, 4).toString(), "PNG");
      assert.equal(bytes.readUInt32BE(16), tile.width);
      assert.equal(bytes.readUInt32BE(20), tile.height);
    }
    assert.equal(data.source.assetId, "50");
    const again = await runCli(args, { XDG_CONFIG_HOME: config });
    assert.equal(again.code, 1, again.stdout);
    assert.equal(fs.readdirSync(dest).length, 4);
    changeSource = true;
    const stale = path.join(root, "stale-tiles");
    const raced = await runCli(
      args.map((argument) => (argument === dest ? stale : argument)),
      {
        XDG_CONFIG_HOME: config,
      },
    );
    assert.equal(JSON.parse(raced.stdout).error.reason, "conflict");
    assert.equal(fs.existsSync(stale), false);
    assert.ok(!fs.readdirSync(root).some((entry) => entry.startsWith(".vc-media-")));
  },
);

test("从授权素材抽帧和修剪，源变化时不发布本地产物", { skip: !hasFFmpeg }, async (context) => {
  const root = tempConfig();
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const source = path.join(root, "source.mp4");
  const made = spawnSync("ffmpeg", [
    "-v",
    "error",
    "-f",
    "lavfi",
    "-i",
    "color=red:s=64x48:r=24",
    "-t",
    "1",
    "-c:v",
    "libx264",
    source,
  ]);
  assert.equal(made.status, 0, made.stderr.toString());
  const bytes = fs.readFileSync(source);
  let revision = 7;
  let changeSource = false;
  const site = await startSite(({ path, headers }) => {
    if (path === "/api/v1/canvas/c1/media/9007199254740993") {
      assert.equal(headers.authorization, "Bearer t");
      if (changeSource) revision++;
      return { body: bytes, contentType: "video/mp4" };
    }
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision,
          payload_json: {
            nodes: [
              {
                id: "shot",
                data: { kind: "video", assetId: revision === 7 ? "9007199254740993" : "51" },
              },
            ],
          },
        },
      });
  });
  context.after(() => site.close());
  const config = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(config, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: config };
  const sourceArgs = ["c1", "shot", "--epoch", "3", "--revision", "7", "--json"];
  const frame = path.join(root, "last.png");
  const framed = await runCli(
    ["canvas", "media", "frame", ...sourceArgs, "--which", "last", "-o", frame],
    env,
  );
  assert.equal(framed.code, 0, framed.stdout);
  const result = JSON.parse(framed.stdout).data;
  assert.equal(result.source.assetId, "9007199254740993");
  assert.equal(result.source.revision, "7");
  assert.equal(fs.readFileSync(frame).subarray(1, 4).toString(), "PNG");
  const trim = path.join(root, "trimmed.mp4");
  const trimmed = await runCli(
    ["canvas", "media", "trim", ...sourceArgs, "--from-ms", "250", "--to-ms", "750", "-o", trim],
    env,
  );
  assert.equal(trimmed.code, 0, trimmed.stdout);
  assert.equal(fs.readFileSync(trim).subarray(4, 8).toString(), "ftyp");
  changeSource = true;
  const stale = path.join(root, "stale.png");
  const raced = await runCli(
    ["canvas", "media", "frame", ...sourceArgs, "--at-ms", "400", "-o", stale],
    env,
  );
  assert.equal(JSON.parse(raced.stdout).error.reason, "conflict");
  assert.equal(fs.existsSync(stale), false);
  assert.ok(!fs.readdirSync(root).some((entry) => entry.startsWith(".vc-media-")));
});
