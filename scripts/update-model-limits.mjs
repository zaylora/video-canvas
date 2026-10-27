#!/usr/bin/env node
// 从 models.dev（MIT 许可，https://github.com/anomalyco/models.dev）生成模型上下文规格快照：
// backend/internal/provider/modelmeta/limits.json，管理端新建 Agent / 文本模型时按模型名自动识别上下文窗口和最大输出。
// 同时生成 modalities.json，供运营识别并手动应用图片、视频输入能力。
//
// 用法：node scripts/update-model-limits.mjs [models.dev 的 api.json 路径]
// 不带参数时从 https://models.dev/api.json 下载。快照里的数据来自第三方，更新后请抽查几个常用模型再提交。
//
// 快照格式：{"模型名": [上下文窗口, 最大输出]}，模型名小写、去掉厂商前缀（最后一个 / 之后的部分）以及 :标签、@后缀。
// 同一个名字在多家服务商下规格不同时取出现次数最多的一组，次数相同取上下文窗口更大的。
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const SOURCE = "https://models.dev/api.json";
const OUT = join(dirname(fileURLToPath(import.meta.url)), "../backend/internal/provider/modelmeta/limits.json");
const MODALITIES_OUT = join(dirname(OUT), "modalities.json");

/** 快照里的键：小写、去厂商前缀、:标签（如 :free）和 @区域或版本（如 @eu、@20250929） */
export function snapshotKey(id) {
  const last = String(id).toLowerCase().split("/").pop() ?? "";
  return last.split(":")[0].split("@")[0].trim();
}

async function load() {
  const path = process.argv[2];
  if (path) return JSON.parse(readFileSync(path, "utf-8"));
  const res = await fetch(SOURCE);
  if (!res.ok) throw new Error(`下载 ${SOURCE} 失败：HTTP ${res.status}`);
  return res.json();
}

const data = await load();
/** 键 → "窗口,输出" → 出现次数 */
const votes = new Map();
/** 输入模态独立投票，不依赖模型是否公开 Token 上限；同票时取较保守的能力。 */
const modalityVotes = new Map();
for (const provider of Object.values(data)) {
  for (const [id, model] of Object.entries(provider.models ?? {})) {
    const key = snapshotKey(model.id ?? id);
    if (!key) continue;
    if (Array.isArray(model.modalities?.input)) {
      const input = model.modalities.input;
      const spec = `${Number(input.includes("image"))},${Number(input.includes("video"))}`;
      const bucket = modalityVotes.get(key) ?? new Map();
      bucket.set(spec, (bucket.get(spec) ?? 0) + 1);
      modalityVotes.set(key, bucket);
    }
    const context = model.limit?.context;
    const output = model.limit?.output;
    if (!Number.isInteger(context) || !Number.isInteger(output) || context <= 0 || output <= 0) continue;
    const spec = `${context},${output}`;
    const bucket = votes.get(key) ?? new Map();
    bucket.set(spec, (bucket.get(spec) ?? 0) + 1);
    votes.set(key, bucket);
  }
}

const out = {};
for (const key of [...votes.keys()].sort()) {
  const best = [...votes.get(key)].sort(
    (a, b) => b[1] - a[1] || Number(b[0].split(",")[0]) - Number(a[0].split(",")[0]),
  )[0][0];
  out[key] = best.split(",").map(Number);
}
writeFileSync(OUT, JSON.stringify(out) + "\n");
console.log(`写入 ${Object.keys(out).length} 个模型 → ${OUT}`);

const modalities = {};
for (const key of [...modalityVotes.keys()].sort()) {
  const best = [...modalityVotes.get(key)].sort(
    (a, b) => b[1] - a[1] || a[0].localeCompare(b[0]),
  )[0][0];
  modalities[key] = best.split(",").map((value) => value === "1");
}
writeFileSync(MODALITIES_OUT, JSON.stringify(modalities) + "\n");
console.log(`写入 ${Object.keys(modalities).length} 个输入模态 → ${MODALITIES_OUT}`);
