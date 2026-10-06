#!/usr/bin/env node
import { EventEmitter } from "node:events";
import readline from "node:readline";
import { runAgent } from "./run.mjs";

/**
 * 进程入口。stdin 第一行是启动参数（JSON），之后每行是一条控制消息：
 *   {"type":"steer","text":"..."}  运行中插话
 *   {"type":"abort"}               中止
 * 日志写 stderr（JSON 行）；结果通过桥交给 Go，进程退出码只表示「有没有跑完并报告」：0 报告了，1 没能报告。
 */
const log = (msg, extra = {}) => process.stderr.write(JSON.stringify({ msg, ...extra }) + "\n");
const control = new EventEmitter();
const rl = readline.createInterface({ input: process.stdin });

let started = false;
rl.on("line", async (line) => {
  if (!line.trim()) return;
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    log("忽略无法解析的输入行");
    return;
  }
  if (!started) {
    started = true;
    try {
      await runAgent(msg, { control, log });
      process.exit(0);
    } catch (e) {
      log("无法向桥报告结果", { error: String(e?.message ?? e) });
      process.exit(1);
    }
  }
  if (msg.type === "steer" && typeof msg.text === "string") control.emit("steer", msg.text);
  else if (msg.type === "abort") control.emit("abort");
});
rl.on("close", () => {
  if (!started) process.exit(1); // 父进程没给启动参数就关了
});
