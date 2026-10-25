import assert from "node:assert/strict";
import { test } from "node:test";
import { serviceMode } from "../src/mode.ts";

test("AGENT_RUNTIME 显式决定模式：durable 是新引擎，http 和 process 都是旧的无状态服务", () => {
  assert.equal(serviceMode({ AGENT_RUNTIME: "durable", AGENT_DB_PATH: "/d/a.sqlite" }), "durable");
  assert.equal(serviceMode({ AGENT_RUNTIME: "http" }), "legacy");
  assert.equal(serviceMode({ AGENT_RUNTIME: "process" }), "legacy");
});

test("Compose 里 AGENT_DB_PATH 恒定设置：显式的 http / process 优先，不会因为有路径就误入 durable", () => {
  assert.equal(serviceMode({ AGENT_RUNTIME: "http", AGENT_DB_PATH: "/data/agent.sqlite" }), "legacy");
  assert.equal(serviceMode({ AGENT_RUNTIME: "process", AGENT_DB_PATH: "/data/agent.sqlite" }), "legacy");
  assert.equal(serviceMode({ AGENT_RUNTIME: "durable", AGENT_DB_PATH: "/data/agent.sqlite" }), "durable");
});

test("没设 AGENT_RUNTIME（或是空串）：沿用旧规则，配了 AGENT_DB_PATH 就是 durable", () => {
  assert.equal(serviceMode({ AGENT_DB_PATH: "/data/agent.sqlite" }), "durable");
  assert.equal(serviceMode({ AGENT_RUNTIME: "", AGENT_DB_PATH: "/data/agent.sqlite" }), "durable");
  assert.equal(serviceMode({}), "legacy");
  assert.equal(serviceMode({ AGENT_RUNTIME: "", AGENT_DB_PATH: "" }), "legacy");
});

test("durable 缺 AGENT_DB_PATH、不认识的值：直接报错并说清楚（不悄悄退回旧模式）", () => {
  assert.throws(() => serviceMode({ AGENT_RUNTIME: "durable" }), /AGENT_DB_PATH/);
  assert.throws(() => serviceMode({ AGENT_RUNTIME: "durable", AGENT_DB_PATH: "" }), /AGENT_DB_PATH/);
  assert.throws(() => serviceMode({ AGENT_RUNTIME: "grpc" }), /AGENT_RUNTIME.*durable.*http.*process|durable.*http.*process/);
});
