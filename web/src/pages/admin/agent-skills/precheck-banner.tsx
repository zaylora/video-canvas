import { CheckCircle2, CircleAlert, TriangleAlert } from "lucide-react";

import type { SkillImportView } from "@/api/admin/agent-skill/type.d";
import { toneClasses, Tag } from "@/components/admin-ui/tag";
import { bannerTone, countIssues, formatSize, planSummary } from "@/utils/admin/agent-skill";
import { cn } from "@/lib/utils";

const TONE = {
  ok: { icon: CheckCircle2, tone: "success", label: "检查通过" },
  warn: { icon: TriangleAlert, tone: "warning", label: "有提示" },
  bad: { icon: CircleAlert, tone: "danger", label: "有错误" },
} as const;

/** 标题里的“新技能 v1 / 新版本 v3 / 无法导入” */
function planLabel(view: SkillImportView): string {
  const { plan } = view;
  if (plan.action === "create") return `新技能 v${plan.version ?? 1}`;
  if (plan.action === "new_version") return `新版本 v${plan.version ?? ""}`;
  return "无法导入";
}

/**
 * 导入第 2 步顶部的横幅（常驻，不随滚动消失）：绿 / 黄 / 红三种语气，图标 + 文字，不只靠颜色。
 * 左侧是技能名、会发生什么、错误与提示数；右侧是文件数、大小（窄屏隐藏）。
 * @param view 预检结果
 */
export function PrecheckBanner({ view }: { view: SkillImportView }) {
  const tone = bannerTone(view);
  const { icon: Icon, tone: tagTone, label } = TONE[tone];
  const count = countIssues(view.issues);
  const files = view.files ?? [];
  const notes = [
    count.error > 0 ? `${count.error} 个错误` : null,
    count.warn > 0 ? `${count.warn} 条提示` : null,
  ].filter(Boolean);
  const stats = [
    { value: files.length, label: "文件" },
    { value: formatSize(view.total_bytes), label: "大小" },
  ];
  return (
    <div
      data-slot="precheck-banner"
      data-tone={tone}
      role={tone === "bad" ? "alert" : "status"}
      className={cn(
        "mx-5 mt-4 flex shrink-0 items-center gap-3 rounded-lg border px-3.5 py-3 max-md:mx-3.5 max-md:mt-3",
        toneClasses[tagTone],
      )}
    >
      <span className="grid size-8 shrink-0 place-items-center rounded-full bg-current/15">
        <Icon className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <h3 className="text-foreground flex flex-wrap items-center gap-2 text-[15px] font-semibold">
          <span className="min-w-0 truncate">
            {view.name || "未识别的技能"} · {planLabel(view)}
          </span>
          <Tag tone={tagTone}>{label}</Tag>
        </h3>
        <p className="text-muted-foreground mt-0.5 text-[13px] break-words">
          {planSummary(view)}
          {notes.length > 0 && (
            <>
              {" · "}
              <span className="tabular-nums">{notes.join("，")}</span>
            </>
          )}
        </p>
      </div>
      <dl className="text-muted-foreground flex shrink-0 gap-4 text-xs max-md:hidden">
        {stats.map((stat) => (
          <div key={stat.label}>
            <dd className="text-foreground text-[15px] font-semibold tabular-nums">{stat.value}</dd>
            <dt>{stat.label}</dt>
          </div>
        ))}
      </dl>
    </div>
  );
}
