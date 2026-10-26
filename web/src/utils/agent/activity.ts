import { toolVerb } from "@/constants/agent";

import type { TimelineItem } from "./timeline";

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

/**
 * 活动行收起后的摘要（设计稿 6.12）：用时 + 按动词首次出现的顺序计数的图标串。
 * text 是同样内容的文字版，给读屏和 tooltip 用，如「已处理 69s · 计划 2 · 读取 1」。
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
  return { seconds, verbs, text };
}
