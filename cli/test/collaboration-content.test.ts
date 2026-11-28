import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

import { ok, runCli, startSite, tempConfig } from "./helpers/site.ts";

test("协作收藏与计划绑定保留超过 JS 安全整数范围的账号和素材 ID", async (context) => {
  const actor = "9007199254740997";
  const asset = "9007199254740999";
  const site = await startSite(({ path: requestPath }) => {
    if (requestPath.endsWith("/me"))
      return { body: Buffer.from(`{"code":0,"data":{"id":${actor}}}`) };
    if (requestPath.endsWith("/collaboration")) {
      return {
        body: Buffer.from(
          `{"code":0,"data":{"epoch":3,"role":"editor","canCopyToLibrary":true,"canvas":{"id":"c1","revision":7,"payload_json":{"nodes":[{"id":"shot","data":{"kind":"image","assetId":${asset}}}]}}}}`,
        ),
      };
    }
    if (requestPath.endsWith("/collaboration/library")) return ok({});
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: root };
  const input = path.join(root, "change.json");
  fs.writeFileSync(
    input,
    '{"changes":[{"kind":"node","id":"shot","fields":{"data.label":"素材"}}]}',
  );
  const plan = await runCli(["canvas", "plan", "c1", "--input", input, "--json"], env);
  assert.equal(plan.code, 0, plan.stdout);
  assert.equal(JSON.parse(plan.stdout).data.actor, actor);
  const result = await runCli(
    ["canvas", "asset", "collect", "c1", "shot", "--asset-id", asset, "--json"],
    env,
  );
  assert.equal(result.code, 0, result.stdout);
  assert.equal(JSON.parse(result.stdout).data.assetId, asset);
  assert.match(site.requests.at(-1)?.raw ?? "", new RegExp(`"asset_id":${asset}(?=[,}])`));
});

test("从授权图谱读取候选结果，选择和挂载通过协作事务提交", async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision: 7,
          payload_json: {
            nodes: [
              { id: "shot", data: { kind: "image", outputs: [{ id: "out1", assetId: "11" }] } },
            ],
          },
        },
      });
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) return ok({ opId: body.opId, epoch: 3, revision: 8 });
  });
  context.after(() => site.close());
  const env = { XDG_CONFIG_HOME: tempConfig({ base: site.base, token: "t" }) };
  const listed = await runCli(["canvas", "output", "list", "c1", "shot", "--json"], env);
  assert.equal(listed.code, 0, listed.stdout);
  assert.deepEqual(JSON.parse(listed.stdout).data.outputs, [{ id: "out1", assetId: "11" }]);
  const selected = await runCli(
    ["canvas", "output", "select", "c1", "shot", "--output-id", "out1", "--json"],
    env,
  );
  assert.equal(selected.code, 0, selected.stdout);
  const attached = await runCli(
    ["canvas", "asset", "replace", "c1", "shot", "--asset-id", "12", "--json"],
    env,
  );
  assert.equal(attached.code, 0, attached.stdout);
  const commits = site.requests.filter(({ method }) => method === "POST");
  assert.deepEqual(
    commits.map(({ body }) => body.changes),
    [
      [{ kind: "node", id: "shot", fields: { "data.activeOutputId": "out1" } }],
      [{ kind: "node", id: "shot", fields: { "data.assetId": "12" } }],
    ],
  );
});

test("下载使用授权画布入口，拒绝覆盖已有本地文件", async (context) => {
  const content = Buffer.from([1, 2, 255, 4]);
  const asset = "9007199254740993";
  const site = await startSite(({ path, headers }) => {
    if (path.endsWith(`/canvas/c1/media/${asset}`) && headers.authorization === "Bearer t") {
      return { body: content, contentType: "video/mp4" };
    }
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const file = path.join(root, "result.mp4");
  const args = ["asset", "download", asset, "--canvas", "c1", "-o", file, "--json"];
  const env = { XDG_CONFIG_HOME: root };
  const denied = await runCli(
    args.map((value) => (value === "c1" ? "other" : value)),
    env,
  );
  assert.equal(denied.code, 1, denied.stdout);
  assert.ok(!fs.existsSync(file));
  const success = await runCli(args, env);
  assert.equal(success.code, 0, success.stdout);
  assert.deepEqual(fs.readFileSync(file), content);
  assert.equal(JSON.parse(success.stdout).data.assetId, asset);
  fs.writeFileSync(file, "已有内容");
  const existing = await runCli(args, env);
  assert.equal(existing.code, 1, existing.stdout);
  assert.equal(fs.readFileSync(file, "utf8"), "已有内容");
});

test("节点参考区分连线和手选素材，替换只提交授权校验所需的素材 ID", async (context) => {
  const largeId = "9007199254740993";
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration")) {
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision: 7,
          payload_json: {
            nodes: [
              { id: "source", data: { kind: "image", label: "参考图" } },
              {
                id: "target",
                data: { kind: "video", params: { images: [largeId], mode: "reference" } },
              },
            ],
            edges: [{ id: "edge-1", source: "source", target: "target" }],
          },
        },
      });
    }
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) return ok({ opId: body.opId, epoch: 3, revision: 8 });
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: root };
  const command = ["canvas", "node", "refs", "c1", "target", "--json"];
  const listed = await runCli(command, env);
  assert.equal(listed.code, 0, listed.stdout);
  assert.deepEqual(JSON.parse(listed.stdout).data, {
    epoch: "3",
    revision: "7",
    linked: [{ edgeId: "edge-1", nodeId: "source", kind: "image", label: "参考图" }],
    manual: { images: [{ assetId: largeId }] },
  });
  const file = path.join(root, "references.json");
  fs.writeFileSync(
    file,
    JSON.stringify({ images: [{ assetId: largeId }], videos: [{ assetId: "23" }] }),
  );
  const update = [...command, "--input", file, "--epoch", "3", "--revision"];
  const stale = await runCli([...update, "6"], env);
  assert.equal(JSON.parse(stale.stdout).error.reason, "conflict");
  const submitted = await runCli([...update, "7"], env);
  assert.equal(submitted.code, 0, submitted.stdout);
  const posts = site.requests.filter(({ method }) => method === "POST");
  assert.deepEqual(
    posts.map(({ body }) => body.changes),
    [
      [
        {
          kind: "node",
          id: "target",
          fields: {
            "data.manualReferences": {
              images: [{ assetId: largeId }],
              videos: [{ assetId: "23" }],
            },
          },
        },
      ],
    ],
  );
});

test("画布节点、连线与分组共用确认版本和原子提交", async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration")) {
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision: 7,
          payload_json: {
            nodes: [
              { id: "a", data: { kind: "script" } },
              { id: "b", data: { kind: "image" } },
            ],
            edges: [],
          },
        },
      });
    }
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) return ok({ opId: body.opId, epoch: 3, revision: 8 });
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = { XDG_CONFIG_HOME: root };
  const version = ["--epoch", "3", "--revision", "7", "--json"];
  const filtered = await runCli(
    ["canvas", "node", "list", "c1", "--kind", "script", "--json"],
    env,
  );
  assert.equal(filtered.code, 0, filtered.stdout);
  assert.deepEqual(
    JSON.parse(filtered.stdout).data.nodes.map(({ id }: { id: string }) => id),
    ["a"],
  );
  for (const args of [
    ["node", "move", "c1", "a", "--at", "80,40"],
    ["edge", "add", "c1", "e", "a", "b"],
    ["group", "add", "c1", "g", "--at", "0,0", "--label", "场景"],
  ]) {
    const result = await runCli(["canvas", ...args, ...version], env);
    assert.equal(result.code, 0, result.stdout);
  }
  const posts = site.requests.filter(({ method }) => method === "POST");
  assert.ok(posts.every(({ body }) => body.baseRevision === 7 && body.epoch === 3));
  assert.deepEqual(
    posts.map(({ body }) => body.changes[0]),
    [
      { kind: "node", id: "a", fields: { position: { x: 80, y: 40 } } },
      { kind: "edge", id: "e", action: "create", value: { source: "a", target: "b" } },
      {
        kind: "group",
        id: "g",
        action: "create",
        value: { position: { x: 0, y: 0 }, data: { label: "场景" } },
      },
    ],
  );
});

test("结构化节点整字段提交锁定读取版本，避免并行更新后继续覆盖", async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration")) {
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision: 7,
          payload_json: {
            nodes: ["character", "director", "storyboard"].map((kind) => ({
              id: kind,
              data: { kind },
            })),
          },
        },
      });
    }
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) {
      return ok({
        opId: body.opId,
        epoch: 3,
        revision: 8,
        conflicts: body.requireBaseRevision ? [{ field: "revision", revision: 8 }] : [],
      });
    }
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const filename = path.join(root, "updated.json");
  fs.writeFileSync(filename, "{}");
  for (const kind of ["character", "director", "storyboard"]) {
    const result = await runCli(
      ["canvas", kind, "set", "c1", kind, "--input", filename, "--revision", "7", "--json"],
      { XDG_CONFIG_HOME: root },
    );
    assert.equal(result.code, 1, result.stdout);
    assert.equal(JSON.parse(result.stdout).error.reason, "conflict");
    assert.equal(site.requests.at(-1)?.body.baseRevision, 7);
    assert.equal(site.requests.at(-1)?.body.requireBaseRevision, true);
  }
});

test("布局只调整选定节点，并在同一批次内写回各自的分组坐标", async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision: 7,
          payload_json: {
            nodes: [
              { id: "group", type: "group", position: { x: 500, y: 120 } },
              { id: "child", parentId: "group", position: { x: 30, y: 20 } },
              { id: "plain", position: { x: 55, y: 160 }, width: 100 },
              { id: "untouched", position: { x: 0, y: 0 } },
            ],
          },
        },
      });
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) return ok({ opId: body.opId, epoch: 3, revision: 8 });
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const result = await runCli(
    [
      "canvas",
      "node",
      "arrange",
      "c1",
      "plain",
      "child",
      "--mode",
      "row",
      "--epoch",
      "3",
      "--revision",
      "7",
      "--json",
    ],
    { XDG_CONFIG_HOME: root },
  );
  assert.equal(result.code, 0, result.stdout);
  const post = site.requests.find(({ method }) => method === "POST");
  assert.equal(post?.body.requireBaseRevision, true);
  assert.deepEqual(post?.body.changes, [
    { kind: "node", id: "plain", fields: { position: { x: 55, y: 140 } } },
    { kind: "node", id: "child", fields: { position: { x: -225, y: 20 } } },
  ]);
});

test("复制媒体节点保留生成配置与入边，排除原任务和产物", async (context) => {
  const site = await startSite(({ path, body }) => {
    if (path.endsWith("/collaboration"))
      return ok({
        epoch: 3,
        role: "editor",
        canvas: {
          id: "c1",
          revision: 7,
          payload_json: {
            nodes: [
              { id: "src", position: { x: 10, y: 10 }, data: { kind: "image", label: "关键帧" } },
              {
                id: "video",
                position: { x: 400, y: 30 },
                data: {
                  kind: "video",
                  label: "镜头",
                  model: "v1",
                  prompt: "镜头前移",
                  params: { images: ["31"], duration: 5 },
                  taskId: "88",
                  assetId: "99",
                  outputs: [{ id: "out" }],
                },
              },
            ],
            edges: [{ id: "incoming", source: "src", target: "video", sourceHandle: "out" }],
          },
        },
      });
    if (path.endsWith("/me")) return ok({ id: 1 });
    if (path.endsWith("/collaboration/ops")) return ok({ opId: body.opId, epoch: 3, revision: 8 });
  });
  context.after(() => site.close());
  const root = tempConfig({ base: site.base, token: "t" });
  context.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const result = await runCli(
    ["canvas", "node", "duplicate", "c1", "video", "--epoch", "3", "--revision", "7", "--json"],
    { XDG_CONFIG_HOME: root },
  );
  assert.equal(result.code, 0, result.stdout);
  const post = site.requests.find(({ method }) => method === "POST");
  assert.equal(post?.body.requireBaseRevision, true);
  const [node, edge] = post?.body.changes ?? [];
  assert.deepEqual(node?.value.data, {
    kind: "video",
    label: "镜头 副本",
    model: "v1",
    prompt: "镜头前移",
    params: { duration: 5 },
    manualReferences: { images: [{ assetId: "31" }] },
  });
  assert.deepEqual(edge?.value, { source: "src", target: node.id, sourceHandle: "out" });
});
