import { Check, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import type { MyPrompt, PromptChoice, PromptSettings, PromptStep } from "@/api/drama/type";
import { ChipPopover } from "@/components/drama/pickers";
import { cn } from "@/lib/utils";
import { PROMPT_STEPS, promptRowState, sameChoice } from "@/utils/drama/prompts";

import type { PromptLibrary } from "./use-prompt-library";

/** 下拉里的分组小标题 */
const GroupLabel = ({ children }: { children: React.ReactNode }) => (
  <div className="text-muted-foreground px-2 pt-2 pb-0.5 text-xs">{children}</div>
);

/** 下拉里的一个可选行 */
function Pick({
  active,
  disabled,
  title,
  onClick,
  children,
  className,
}: {
  active?: boolean;
  disabled?: boolean;
  title?: string;
  onClick?: () => void;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      title={title}
      onClick={onClick}
      className={cn(
        "hover:bg-muted focus-visible:ring-ring/50 flex min-h-10 min-w-0 items-center gap-2 rounded-lg px-2.5 text-left text-sm outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent",
        active && "bg-muted font-medium",
        className,
      )}
    >
      {children}
      {active && <Check className="ml-auto size-4 shrink-0" />}
    </button>
  );
}

/**
 * 四行提示词设置（剧本改编 / 资产拆解 / 资产图提示词 / 视频提示词）：每行一个胶囊下拉，
 * 内容是「内置（推荐）」「我的提示词（含新建 / 编辑 / 删除）」「技能」。
 * 开拍设置弹窗与工作台「提示词设置」共用；新建 / 编辑交给调用方（弹窗内侧滑到编辑器）。
 * 剧本型输入的「剧本改编」置灰。
 */
export function PromptRows({
  kind,
  value,
  library,
  saved,
  onChange,
  onEdit,
}: {
  /** 确认的原文类型；script 时剧本改编置灰 */
  kind: "script" | "novel" | null;
  /** 各环节当前选择 */
  value: Record<PromptStep, PromptChoice>;
  /** 提示词库 */
  library: PromptLibrary;
  /**
   * 已建剧的提示词设置：选择没改动时直接用后端给的展示名，来源已被删 / 停用（missing）的行标红提示；
   * 选了别的就按提示词库现算
   */
  saved?: PromptSettings;
  /** 选了别的提示词 */
  onChange: (step: PromptStep, choice: PromptChoice) => void;
  /** 新建（item 为 null）或编辑某条我的提示词 */
  onEdit: (step: PromptStep, item: MyPrompt | null) => void;
}) {
  const removeMine = async (step: PromptStep, item: MyPrompt) => {
    await library.remove(item);
    if (sameChoice(value[step], { source: "mine", id: item.id })) {
      onChange(step, { source: "builtin" });
    }
    toast.success("已删除，已建的短剧不受影响");
  };

  return (
    <div className="grid" data-slot="prompt-rows">
      {PROMPT_STEPS.map(({ key, name }) => {
        const mine = library.mine[key];
        const skills = library.skills[key];
        const computed = promptRowState(key, kind, value[key], mine, skills);
        const keep = saved && sameChoice(value[key], saved[key]) ? saved[key] : null;
        const row =
          keep && !computed.disabledReason ? { ...computed, label: keep.label } : computed;
        const flagged = keep ? !!keep.missing : row.missing;
        return (
          <div key={key} className="flex min-h-12 items-center gap-3 px-3">
            <span className="text-muted-foreground w-24 shrink-0 text-sm">{name}</span>
            <ChipPopover
              variant="field"
              label={row.label}
              tone={flagged ? "warn" : undefined}
              disabled={!!row.disabledReason}
              title={
                row.disabledReason ??
                (flagged ? "来源已被删除或停用，已建的任务照常使用快照" : undefined)
              }
              width="w-80"
            >
              {(close) => (
                <div className="grid gap-0.5">
                  <Pick
                    active={value[key].source === "builtin"}
                    onClick={() => {
                      onChange(key, { source: "builtin" });
                      close();
                    }}
                  >
                    内置
                    <span className="bg-muted text-muted-foreground rounded-full px-1.5 text-[10px] leading-4">
                      推荐
                    </span>
                  </Pick>
                  <GroupLabel>我的提示词</GroupLabel>
                  {mine.map((item) => (
                    <div key={item.id} className="flex items-center gap-0.5">
                      <Pick
                        className="flex-1"
                        active={sameChoice(value[key], { source: "mine", id: item.id })}
                        onClick={() => {
                          onChange(key, { source: "mine", id: item.id });
                          close();
                        }}
                      >
                        <span className="truncate">{item.name}</span>
                      </Pick>
                      <button
                        type="button"
                        title="编辑"
                        aria-label={`编辑 ${item.name}`}
                        onClick={() => {
                          close();
                          onEdit(key, item);
                        }}
                        className="text-muted-foreground hover:bg-muted hover:text-foreground grid size-8 shrink-0 place-items-center rounded-lg"
                      >
                        <Pencil className="size-3.5" />
                      </button>
                      <button
                        type="button"
                        title="删除"
                        aria-label={`删除 ${item.name}`}
                        onClick={() => void removeMine(key, item)}
                        className="text-muted-foreground hover:bg-muted hover:text-destructive grid size-8 shrink-0 place-items-center rounded-lg"
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    </div>
                  ))}
                  <Pick
                    onClick={() => {
                      close();
                      onEdit(key, null);
                    }}
                  >
                    <Plus className="size-4" />
                    新建
                  </Pick>
                  <GroupLabel>技能</GroupLabel>
                  {skills.length === 0 ? (
                    <Pick disabled title="后台还没有给技能标这个环节">
                      暂无适用技能
                    </Pick>
                  ) : (
                    skills.map((skill) => (
                      <Pick
                        key={skill.name}
                        active={sameChoice(value[key], { source: "skill", skill: skill.name })}
                        onClick={() => {
                          onChange(key, { source: "skill", skill: skill.name });
                          close();
                        }}
                      >
                        <span className="min-w-0">
                          <span className="block truncate">{skill.title}</span>
                          <span className="text-muted-foreground block truncate text-xs font-normal">
                            {skill.description}
                          </span>
                        </span>
                      </Pick>
                    ))
                  )}
                </div>
              )}
            </ChipPopover>
          </div>
        );
      })}
    </div>
  );
}
