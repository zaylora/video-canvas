// 读轨迹包 / 运行目录的小工具，diff 和 to-case 共用。
// 目录既可以是 harness run 的输出目录，也可以是后台下载解压的轨迹包（.harness/imports/<runId>/）。
// 里面的内容是线上用户数据，当不可信数据对待：只做 JSON.parse，不执行、不 eval。

import fs from "node:fs";
import path from "node:path";

/** 目录或文件有问题（不存在、缺文件、JSON 坏了）。message 面向人，直接打印，不带堆栈。 */
export class BundleError extends Error {}

/** 目录必须存在。 */
export function requireDir(dir, what = "目录") {
  if (!fs.existsSync(dir)) throw new BundleError(`${what}不存在：${dir}`);
  if (!fs.statSync(dir).isDirectory()) throw new BundleError(`${what}不是目录：${dir}`);
}

/** 读 JSON 文件。文件缺失或 JSON 坏了抛 BundleError（说明是哪个文件）。 */
export function readJsonFile(file) {
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (e) {
    throw new BundleError(`读不了 ${file}：${e.message}`);
  }
  try {
    return JSON.parse(text);
  } catch (e) {
    throw new BundleError(`${file} 不是合法的 JSON（${e.message}）`);
  }
}

/** 读 JSONL：一行一个 JSON，空行忽略。某一行坏了就报行号。 */
export function readJsonlFile(file) {
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (e) {
    throw new BundleError(`读不了 ${file}：${e.message}`);
  }
  const rows = [];
  text.split(/\r?\n/).forEach((line, i) => {
    if (line.trim() === "") return;
    try {
      rows.push(JSON.parse(line));
    } catch (e) {
      throw new BundleError(`${file} 第 ${i + 1} 行不是合法的 JSON（${e.message}）`);
    }
  });
  return rows;
}

/** 事件文件：harness 自己补拉的 events.pulled.jsonl 优先（与 report 命令一致），其次后端导出的 events.jsonl；都没有返回 null。 */
export function findEventsFile(dir) {
  return ["events.pulled.jsonl", "events.jsonl"].map((f) => path.join(dir, f)).find((f) => fs.existsSync(f)) ?? null;
}

/** 事件里的 run_id：只有当事件确实带着 meta 里的 run_id 时才按它过滤，否则不过滤（单次运行的导出本来就只含这一次）。 */
export function runIdFilter(events, runId) {
  return runId && events.some((e) => e.run_id === runId) ? runId : undefined;
}
