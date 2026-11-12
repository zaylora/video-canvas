import { Box, BookOpen, CodeXml, Lock, Undo2 } from "lucide-react";

import type {
  StepCandidateInfo,
  StepDetail,
  StepMedia,
  TrialCandidate,
} from "@/api/admin/steps/type";
import { FoldableCode } from "@/components/admin-ui/foldable-code";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import {
  bindingLabel,
  candidateBadges,
  candidateLabel,
  candidateOfHistory,
  sameCandidate,
} from "@/utils/admin/steps";

/** 输入媒体的展示名 */
export const MEDIA_LABEL: Record<StepMedia, string> = {
  none: "无（纯文本）",
  images: "看图",
  video: "看视频（退回看图 + 截帧）",
};

/** 提示词拼接顺序（协议层固定，方法层替换） */
const PROMPT_ORDER = `## 本步职责        ← 协议
## 输入            ← 协议（变量 JSON + 多模态附件）
## 方法            ← 方法层（内置正文，或【写作方法：技能 vN】<skill>…</skill>）
## 输出格式        ← 协议（由 Schema 生成）
## 校验规则        ← 协议`;

/** 小节：标题 + 内容 */
function Section({
  title,
  aside,
  children,
}: {
  title: string;
  aside?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="grid gap-2">
      <h3 className="flex items-center gap-2 text-sm font-medium">
        {title}
        {aside}
      </h3>
      {children}
    </section>
  );
}

/** 灰底的内容框 */
const BOX = "bg-chrome-hover ring-chrome-border rounded-2xl p-3.5 text-sm leading-relaxed ring-1";

/** 协议标签（只读）：三层关系、本步职责、输入变量、输出 Schema、校验规则和提示词拼接顺序 */
export function ContractTab({ detail }: { detail: StepDetail }) {
  const effective = detail.effective;
  return (
    <div className="grid gap-5">
      <div className="grid gap-2">
        <div className={cn(BOX, "flex items-start gap-2")}>
          <Lock className="mt-0.5 size-4 shrink-0" />
          <span>
            <b className="font-medium">协议层</b>
            <span className="text-muted-foreground ml-2">
              职责、输入、输出 Schema、校验——固定，代码维护
            </span>
          </span>
        </div>
        <div className={cn(BOX, "ring-beam/40 flex items-start gap-2")}>
          <BookOpen className="mt-0.5 size-4 shrink-0" />
          <span>
            <b className="font-medium">方法层</b>
            <span className="text-muted-foreground ml-2">
              当前：
              {effective.fallback ? "内置方法（已退回）" : bindingLabel(detail.binding)}——可替换
            </span>
          </span>
        </div>
        <div className={cn(BOX, "flex items-start gap-2")}>
          <CodeXml className="mt-0.5 size-4 shrink-0" />
          <span>
            <b className="font-medium">格式化层</b>
            <span className="text-muted-foreground ml-2">
              脚本 → 视频提示词，按模型写法拼装——固定，代码维护
            </span>
          </span>
        </div>
      </div>

      <Section title="本步职责">
        <div className={BOX}>{detail.duty}</div>
      </Section>

      <Section title="输入" aside={<Tag>{MEDIA_LABEL[detail.media]}</Tag>}>
        <div className={cn(BOX, "p-0")}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>变量</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>说明</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {detail.inputs.map((input) => (
                <TableRow key={input.name}>
                  <TableCell className="font-mono text-xs">{input.name}</TableCell>
                  <TableCell className="text-muted-foreground">{input.type}</TableCell>
                  <TableCell>{input.desc}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Section>

      <Section title="输出 JSON Schema">
        <FoldableCode text={detail.schema} defaultOpen maxChars={4000} />
      </Section>

      <Section title="校验规则">
        <div className={BOX}>
          <div className="text-muted-foreground mb-1 text-xs">
            硬错误（带原因重写一次，仍不过则该环节失败）
          </div>
          <ul className="list-disc pl-5">
            {detail.rules.hard.map((rule) => (
              <li key={rule}>{rule}</li>
            ))}
          </ul>
          <div className="text-muted-foreground mt-3 mb-1 text-xs">
            软问题（通过，但在用户界面标红，改掉才能出片）
          </div>
          <ul className="list-disc pl-5">
            {detail.rules.soft.map((rule) => (
              <li key={rule}>{rule}</li>
            ))}
          </ul>
        </div>
      </Section>

      <Section title="提示词拼接顺序">
        <FoldableCode text={PROMPT_ORDER} defaultOpen />
      </Section>
    </div>
  );
}

/** 一个候选在列表里的单选行：内置方法或技能（带版本选择和角标） */
function CandidateRow({
  info,
  selected,
  current,
  version,
  onSelect,
  onVersion,
}: {
  info: StepCandidateInfo;
  selected: boolean;
  /** 就是当前生效的绑定 */
  current: boolean;
  /** 选中技能时的版本 */
  version: number | undefined;
  onSelect: () => void;
  onVersion: (version: number) => void;
}) {
  const skill = info.source === "skill" ? info : null;
  const disabled = !!skill && !skill.enabled;
  return (
    <div
      className={cn(
        "ring-chrome-border flex items-center gap-2.5 rounded-2xl px-3.5 py-2.5 ring-1 transition-colors duration-120",
        selected ? "bg-chrome-hover ring-foreground/25" : "hover:bg-chrome-hover",
        disabled && "opacity-60",
      )}
    >
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        disabled={disabled}
        title={disabled ? "技能已停用" : undefined}
        onClick={onSelect}
        className="focus-visible:ring-ring/50 flex min-w-0 flex-1 items-center gap-2.5 rounded-lg text-left outline-none focus-visible:ring-2 disabled:cursor-not-allowed"
      >
        <span
          aria-hidden
          className={cn(
            "grid size-4 shrink-0 place-items-center rounded-full border",
            selected ? "border-foreground" : "border-border",
          )}
        >
          {selected && <span className="bg-foreground size-2 rounded-full" />}
        </span>
        <span className="flex min-w-0 flex-wrap items-center gap-1.5 text-sm">
          {skill ? <BookOpen className="size-4" /> : <Box className="size-4" />}
          <b className="font-medium">{skill ? skill.skill_name : "内置方法"}</b>
          {skill && <span className="text-muted-foreground text-xs">{skill.skill_title}</span>}
          {candidateBadges(info).map((badge) => (
            <Tag
              key={badge}
              tone={badge === "会截断" ? "warning" : "neutral"}
              title={
                badge === "仅用正文"
                  ? "技能里的脚本不执行"
                  : badge === "会截断"
                    ? "正文超出 16,000 字的部分会截断"
                    : undefined
              }
            >
              {badge}
            </Tag>
          ))}
          {current && <Tag tone="success">当前</Tag>}
        </span>
      </button>
      {skill && selected && (
        <select
          aria-label={`${skill.skill_name} 版本`}
          value={version ?? skill.active_version}
          onChange={(event) => onVersion(Number(event.target.value))}
          className="bg-chrome-hover ring-chrome-border focus-visible:ring-ring/50 h-8 rounded-full px-3 text-xs ring-1 outline-none focus-visible:ring-2"
        >
          {skill.versions.map((v) => (
            <option key={v} value={v}>
              v{v}
              {v === skill.active_version ? "（最新）" : ""}
            </option>
          ))}
        </select>
      )}
    </div>
  );
}

/**
 * 方法标签：当前生效的方法、可换的候选（内置 + 已导入的技能，每个技能选版本，
 * 带「仅用正文」「会截断」「已停用」角标）、绑定历史（可恢复，恢复也要先试跑）。
 */
export function MethodTab({
  detail,
  candidate,
  onCandidate,
  onRestore,
}: {
  detail: StepDetail;
  candidate: TrialCandidate;
  onCandidate: (candidate: TrialCandidate) => void;
  /** 恢复到历史里的某个方法：选中它并去试跑 */
  onRestore: (candidate: TrialCandidate) => void;
}) {
  const fallback = !!detail.effective.fallback;
  return (
    <div className="grid gap-5">
      <Section title="当前生效">
        <div className={cn(BOX, "flex items-center gap-2.5")}>
          {detail.effective.source === "builtin" ? (
            <Box className="size-4" />
          ) : (
            <BookOpen className="size-4" />
          )}
          <div className="min-w-0 flex-1">
            <div className="font-medium">
              {fallback ? "内置方法" : bindingLabel(detail.binding)}
            </div>
            <div className="text-muted-foreground text-xs">
              {fallback
                ? `原绑定 ${bindingLabel(detail.binding)} 的技能已停用，自动退回`
                : detail.binding.source === "builtin"
                  ? "随版本发布，改写自 daihuo-fanpai / HiAPIAI 等（见 NOTICE.md）"
                  : `已钉死版本 v${detail.binding.version}`}
            </div>
          </div>
          {fallback ? <Tag tone="warning">已退回内置</Tag> : <Tag tone="success">生效中</Tag>}
        </div>
      </Section>

      <Section title="换一个方法">
        <div role="radiogroup" aria-label="候选方法" className="grid gap-1.5">
          {detail.candidates.map((info) => {
            const key = info.source === "builtin" ? "builtin" : info.skill_name;
            const selected =
              candidate.source === info.source &&
              (info.source === "builtin" || candidate.skill_name === info.skill_name);
            const asCandidate: TrialCandidate =
              info.source === "builtin"
                ? { source: "builtin" }
                : { source: "skill", skill_name: info.skill_name, version: info.active_version };
            return (
              <CandidateRow
                key={key}
                info={info}
                selected={selected}
                current={
                  !fallback &&
                  (info.source === "builtin"
                    ? detail.binding.source === "builtin"
                    : sameCandidate(detail.binding, {
                        source: "skill",
                        skill_name: info.skill_name,
                        version: detail.binding.version,
                      }))
                }
                version={selected ? candidate.version : undefined}
                onSelect={() => !selected && onCandidate(asCandidate)}
                onVersion={(version) => onCandidate({ ...candidate, version })}
              />
            );
          })}
        </div>
      </Section>

      <Section title="绑定历史">
        <div className={cn(BOX, "grid gap-1.5 p-2.5")}>
          {detail.history.length === 0 && (
            <div className="text-muted-foreground px-1 py-1">还没有换过方法</div>
          )}
          {[...detail.history].reverse().map((item) => {
            const from = candidateOfHistory(item.from);
            const to = candidateOfHistory(item.to);
            const changed = from && to && !sameCandidate(from, to);
            return (
              <div key={item.id} className="flex items-center gap-2 px-1 py-0.5 text-sm">
                <span className="text-muted-foreground w-28 shrink-0 text-xs tabular-nums">
                  {new Date(item.created_at).toLocaleString("zh-CN", {
                    month: "2-digit",
                    day: "2-digit",
                    hour: "2-digit",
                    minute: "2-digit",
                  })}
                </span>
                <span className="min-w-0 flex-1">
                  {bindingLabel(item.from)} → <b className="font-medium">{bindingLabel(item.to)}</b>
                </span>
                <span className="text-muted-foreground text-xs">{item.actor}</span>
                {changed && from && (
                  <Button variant="ghost" size="xs" onClick={() => onRestore(from)}>
                    <Undo2 />
                    恢复
                  </Button>
                )}
              </div>
            );
          })}
        </div>
      </Section>
    </div>
  );
}

/** 候选的展示名（抽屉底部和试跑标签用） */
export { candidateLabel };
