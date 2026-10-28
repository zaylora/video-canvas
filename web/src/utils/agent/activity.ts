import { toolVerb } from "@/constants/agent";

import type { TimelineItem, ToolCall } from "./timeline";

type Activity = Pick<
  Extract<TimelineItem, { type: "activity" }>,
  "tools" | "status" | "startedAt" | "endedAt"
>;

/** 动作图标的键：组件按它取 lucide 图标；不认识的动词用 other */
export type VerbIcon =
  | "read"
  | "edit"
  | "arrange"
  | "delete"
  | "look"
  | "plan"
  | "ask"
  | "models"
  | "generate"
  | "task"
  | "skill"
  | "script"
  | "video"
  | "frame"
  | "search"
  | "web"
  | "other";

/** 动词 → 图标键。动词来自后端 tool_registry 与 codemode，前端只认这一张表 */
const VERB_ICONS: Record<string, VerbIcon> = {
  读取: "read",
  修改: "edit",
  整理: "arrange",
  删除: "delete",
  看图: "look",
  计划: "plan",
  提问: "ask",
  查模型: "models",
  生成: "generate",
  查任务: "task",
  技能: "skill",
  脚本: "script",
  看视频: "video",
  截帧: "frame",
  搜索: "search",
  读网页: "web",
};

/** 动词 → 兜底动作短语：模型写的说明还没到（或没有）时，活动行收起后显示它 */
const VERB_PHRASES: Record<string, string> = {
  读取: "读取了画布",
  修改: "修改了画布",
  整理: "整理了布局",
  删除: "申请了删除",
  看图: "查看了图片",
  计划: "更新了计划",
  提问: "向你提了问题",
  查模型: "查看了可用模型",
  生成: "申请了生成",
  查任务: "查询了任务",
  技能: "查阅了技能",
  脚本: "运行了脚本",
  看视频: "查看了视频",
  截帧: "截取了视频帧",
  搜索: "搜索了网页",
  读网页: "读取了网页",
};

/** 主图标的优先级：一段里有多种动作时，取排在最前面的那种（改动和产出比查看更重要） */
const VERB_PRIORITY = [
  "生成",
  "删除",
  "修改",
  "截帧",
  "整理",
  "看视频",
  "看图",
  "搜索",
  "读网页",
  "脚本",
  "提问",
  "计划",
  "技能",
  "查模型",
  "查任务",
  "读取",
];

/** 短语最多写几种动作，再多折成「等 N 项」 */
const PHRASE_KINDS = 3;

/**
 * 兜底动作短语：按动词首次出现的顺序，同一动作多次写「N 次」；超过 3 种动作只写前 3 种加「等 N 项」（N 是动作种类数）。
 * 如「读取了画布、搜索了网页 2 次、修改了画布」。
 */
function phraseOf(verbs: { verb: string; count: number }[]) {
  const parts = verbs.map(
    ({ verb, count }) =>
      (VERB_PHRASES[verb] ?? `调用了「${verb}」`) + (count > 1 ? ` ${count} 次` : ""),
  );
  return parts.length > PHRASE_KINDS
    ? `${parts.slice(0, PHRASE_KINDS).join("、")}等 ${parts.length} 项`
    : parts.join("、");
}

/**
 * 活动行收起后的摘要（设计稿 6.12 与「Agent聊天框改进」6.2）：
 * icon 是这一段的主图标（优先级最高的动作）；phrase 是兜底动作短语，模型的说明到达前显示它；
 * verbs 是按动词首次出现的顺序计数的图标串，text 是文字版（给读屏和 tooltip 用，如「已处理 69s · 计划 2 · 读取 1」）。
 */
export function activitySummary(item: Activity) {
  const counts = new Map<string, number>();
  for (const t of item.tools) {
    const verb = toolVerb(t.name, t.verb, t.label);
    counts.set(verb, (counts.get(verb) ?? 0) + 1);
  }
  const verbs = [...counts].map(([verb, count]) => ({
    verb,
    icon: VERB_ICONS[verb] ?? ("other" as VerbIcon),
    count,
  }));
  const seconds =
    item.startedAt !== null && item.endedAt !== null
      ? Math.max(1, Math.round((item.endedAt - item.startedAt) / 1000))
      : null;
  const head = item.status === "aborted" ? "已中止" : seconds ? `已处理 ${seconds}s` : "已处理";
  const text = [head, ...verbs.map((v) => `${v.verb} ${v.count}`)].join(" · ");
  const main = VERB_PRIORITY.find((v) => counts.has(v)) ?? [...counts.keys()][0];
  const icon: VerbIcon = (main && VERB_ICONS[main]) || "other";
  return {
    seconds,
    verbs,
    text,
    icon,
    phrase: phraseOf([...counts].map(([verb, count]) => ({ verb, count }))),
  };
}

/** 一次工具调用的动作图标键：和活动行摘要用同一张动词表 */
export function toolIcon(tool: Pick<ToolCall, "name" | "verb" | "label">): VerbIcon {
  return VERB_ICONS[toolVerb(tool.name, tool.verb, tool.label)] ?? "other";
}

/** 耗时的显示：60 秒以内写「12s」，再长写「1m 12s」 */
export function formatSeconds(seconds: number) {
  return seconds >= 60 ? `${Math.floor(seconds / 60)}m ${seconds % 60}s` : `${seconds}s`;
}
