import type { ConfigIssue } from "@/api/admin/ai/type";

import { MODEL_KIND_LABEL } from "./model-body";
import type { TraceStepView } from "./trace";

/**
 * 一键复制“修复信息”：把出错信息拼成一段纯文本，用户复制后直接发给 AI，让它按插件契约修好插件。
 * 三个入口各有一个拼装函数：上传预检未通过、模型试跑失败、渠道连通性检查不通过。
 * 追踪内容后端已经脱敏，这里不再另外处理密钥；拿不到的信息直接省略，不编造。
 */

/** 单个块（输入、输出、响应体、错误说明等）的字节上限：2KB */
export const REPAIR_BLOCK_MAX_BYTES = 2048;

/** 整段文本的字节上限：16KB */
export const REPAIR_TOTAL_MAX_BYTES = 16384;

/** 内容被截断处追加的标记 */
export const TRUNCATED_MARK = "…（已截断）";

/** 复制修复信息失败时的提示：这段文本不在界面上，用户没有手动选取的退路，所以要明说 */
export const REPAIR_COPY_FAILED =
  "复制失败：浏览器不允许写入剪贴板。请用 https 访问后台，或在浏览器地址栏允许剪贴板权限后重试。";

/** 每段修复信息的开头：交代要做什么，并说明版本号不用管 */
export const REPAIR_INTRO =
  "我的 video-canvas 协议插件出错了，请按插件契约修复，输出完整的 .js 文件（版本号不用改，上传时会自动递增）。";

/** 修复信息里的插件：key 拿不到就整行省略；版本号拿不到只省略版本号 */
export type RepairPlugin = {
  /** 插件 key */
  key?: string | null;
  /** 版本号（不带 v） */
  version?: string | null;
};

/** 块的标签：与 trace.ts 的 toStepView 给块起的名字一致，报告按它挑要写的块 */
const BLOCK_LABEL = {
  input: "输入",
  output: "输出",
  requestBody: "请求体",
  responseBody: "响应体",
} as const;

const encoder = new TextEncoder();

/** 文本的 UTF-8 字节数 */
const utf8Length = (text: string) => encoder.encode(text).length;

const MARK_BYTES = utf8Length(TRUNCATED_MARK);

/** 一个码点的 UTF-8 字节数；孤立代理项按 3 字节算，与 TextEncoder 编成 U+FFFD 一致 */
const codePointBytes = (codePoint: number) =>
  codePoint < 0x80 ? 1 : codePoint < 0x800 ? 2 : codePoint < 0x10000 ? 3 : 4;

/**
 * 按 UTF-8 字节数截断文本。没超限原样返回；超限时留下尽可能长的前缀并追加“…（已截断）”，
 * 前缀和标记加起来不超过 maxBytes。按码点整个取舍，不会把多字节字符（汉字、emoji）切成两半。
 * @param text 原文
 * @param maxBytes 字节上限（含追加的标记）；小于标记本身时只返回标记
 * @returns 截断后的文本
 */
export function truncateUtf8(text: string, maxBytes: number): string {
  if (utf8Length(text) <= maxBytes) return text;
  const budget = maxBytes - MARK_BYTES;
  let used = 0;
  let end = 0;
  while (end < text.length) {
    const codePoint = text.codePointAt(end)!;
    const size = codePointBytes(codePoint);
    if (used + size > budget) break;
    used += size;
    end += codePoint > 0xffff ? 2 : 1;
  }
  return text.slice(0, end) + TRUNCATED_MARK;
}

/** 统一换行并去掉首尾空白 */
const clean = (text: string | null | undefined) => (text ?? "").replace(/\r\n?/g, "\n").trim();

/** 非空行用换行连起来，空值丢掉 */
const lines = (...items: Array<string | null | undefined | false>) =>
  items.filter(Boolean).join("\n");

/** 给每个非空行加前缀（空行不加，避免行尾空白） */
const indentLines = (text: string, pad: string) =>
  text
    .split("\n")
    .map((line) => (line ? pad + line : line))
    .join("\n");

/**
 * 一个带标签的块：内容单行就写在标签后面，多行则标签独占一行、内容比标签多缩进两格。
 * 内容超过 2KB 先截断，标签上标明“截断到 2KB”，末尾有“…（已截断）”。内容为空返回 null。
 */
function field(label: string, raw: string | null | undefined, indent = ""): string | null {
  const text = clean(raw);
  if (!text) return null;
  const cut = truncateUtf8(text, REPAIR_BLOCK_MAX_BYTES);
  const name = cut === text ? label : `${label}（截断到 ${REPAIR_BLOCK_MAX_BYTES / 1024}KB）`;
  if (!cut.includes("\n")) return `${indent}${name}：${cut}`;
  return `${indent}${name}：\n${indentLines(cut, `${indent}  `)}`;
}

/** 拼上开头说明，各段之间空一行；全文超过 16KB 时从尾部截断，开头的关键信息保留 */
function compose(...sections: string[]): string {
  const text = [REPAIR_INTRO, ...sections].filter(Boolean).join("\n\n");
  return truncateUtf8(text, REPAIR_TOTAL_MAX_BYTES);
}

/** “插件：yswg-image v1.3.1”；key 拿不到整行省略 */
function pluginLine(plugin: RepairPlugin | null | undefined): string | null {
  const key = clean(plugin?.key);
  if (!key) return null;
  const version = clean(plugin?.version);
  return `插件：${key}${version ? ` v${version}` : ""}`;
}

/** 一条预检问题：“- 路径：说明”，说明有多行时续行缩进两格；路径为空表示整个文件 */
function issueLine(issue: ConfigIssue): string {
  const [first, ...rest] = truncateUtf8(clean(issue.message), REPAIR_BLOCK_MAX_BYTES).split("\n");
  const head = `- ${clean(issue.path) || "（整个文件）"}：${first}`;
  return [head, ...rest.map((line) => (line ? `  ${line}` : line))].join("\n");
}

/**
 * 选出要写进修复信息的步骤：追踪里标为失败的步骤；一个都没有（钩子本身执行成功，
 * 但宿主校验它的返回值不合规时钩子步骤没有 error）就取最后一个步骤；追踪为空返回空数组。
 * @param steps 追踪的展示模型（toTraceView 的结果）
 * @returns 要写进文本的步骤，保持原顺序和原序号
 */
export function pickFailedSteps(steps: readonly TraceStepView[]): TraceStepView[] {
  const failed = steps.filter((step) => step.failed);
  if (failed.length > 0) return failed;
  const last = steps[steps.length - 1];
  return last ? [last] : [];
}

/**
 * 一个步骤：“[序号] 标题”，有出错信息先写出错信息。
 * 钩子步骤再写输入、输出、插件日志（utils.log）；http 步骤再写状态码、请求体、响应体，不写请求头（噪声）。
 */
function stepText(step: TraceStepView): string {
  const block = (label: string) => step.blocks.find((item) => item.label === label)?.text;
  const pad = "  ";
  const head = [`[${step.index}] ${step.title}`, field("错误", step.error, pad)];
  if (step.kind === "hook") {
    return lines(
      ...head,
      field("输入", block(BLOCK_LABEL.input), pad),
      field("输出", block(BLOCK_LABEL.output), pad),
      field("插件日志", step.logs.join("\n"), pad),
    );
  }
  if (step.kind === "http") {
    return lines(
      ...head,
      step.status !== undefined && `${pad}状态码：${step.status}`,
      field("请求体", block(BLOCK_LABEL.requestBody), pad),
      field("响应体", block(BLOCK_LABEL.responseBody), pad),
    );
  }
  return lines(...head);
}

/**
 * 上传预检未通过的修复信息：阶段“上传预检” + 每个问题一行“- 路径：说明”。
 * 预检未通过时上传结果里没有版本对象，插件 key 和代码里声明的版本号通常拿不到，拿不到的就省略。
 * @param issues 预检问题（界面上显示的那份）
 * @param pluginKey 插件 key
 * @param declaredVersion 代码里声明的版本号
 * @returns 可以直接发给 AI 的纯文本
 */
export function buildUploadRepairReport({
  issues,
  pluginKey,
  declaredVersion,
}: {
  issues: readonly ConfigIssue[];
  pluginKey?: string | null;
  declaredVersion?: string | null;
}): string {
  const key = clean(pluginKey);
  const declared = clean(declaredVersion);
  const plugin = key
    ? `插件：${key}${declared ? `（代码里的版本 ${declared}）` : ""}`
    : declared && `代码里的版本：${declared}`;
  return compose(
    lines(plugin, "阶段：上传预检"),
    issues.length > 0 ? lines("问题：", ...issues.map(issueLine)) : "",
  );
}

/**
 * 模型试跑失败的修复信息：插件与版本、阶段“试跑”（模型种类与 key）、错误码和错误文案、失败步骤。
 * 能拿到哪些写哪些，拿不到的省略。失败步骤的选择规则见 pickFailedSteps。
 * @param plugin 模型所用渠道的插件与版本
 * @param modelKey 模型 key
 * @param kind 模型种类（image / video / text / audio）
 * @param errorCode 试跑任务的错误码
 * @param errorMessage 试跑任务的错误文案
 * @param steps 追踪的展示模型；追踪没取到或为空就传空数组
 * @returns 可以直接发给 AI 的纯文本
 */
export function buildRunRepairReport({
  plugin,
  modelKey,
  kind,
  errorCode,
  errorMessage,
  steps,
}: {
  plugin?: RepairPlugin | null;
  modelKey?: string | null;
  kind?: string | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  steps: readonly TraceStepView[];
}): string {
  const model = clean(modelKey);
  const kindText = clean(kind);
  const kindName = kindText ? `${MODEL_KIND_LABEL[kindText] ?? kindText}模型` : "";
  const target = kindName ? [kindName, model].filter(Boolean).join(" ") : model && `模型 ${model}`;
  const failed = pickFailedSteps(steps);
  return compose(
    lines(
      pluginLine(plugin),
      `阶段：试跑${target ? `（${target}）` : ""}`,
      field("错误", [clean(errorCode), clean(errorMessage)].filter(Boolean).join(" · ")),
    ),
    failed.length > 0 ? lines("失败步骤：", failed.map(stepText).join("\n\n")) : "",
  );
}

/**
 * 渠道连通性检查不通过的修复信息：插件与版本、阶段“渠道连通性检查”、检查返回的说明。
 * @param plugin 渠道所用的插件与版本
 * @param message 检查返回的说明（ChannelCheckResult.message）
 * @returns 可以直接发给 AI 的纯文本
 */
export function buildChannelCheckRepairReport({
  plugin,
  message,
}: {
  plugin?: RepairPlugin | null;
  message?: string | null;
}): string {
  return compose(lines(pluginLine(plugin), "阶段：渠道连通性检查", field("错误", message)));
}
