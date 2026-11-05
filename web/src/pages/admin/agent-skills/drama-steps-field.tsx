import { useState } from "react";
import { Check } from "lucide-react";

import type { DramaStepKey, SkillItem } from "@/api/admin/agent-skill/type.d";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { PROMPT_STEPS } from "@/utils/drama/prompts";

/** 按固定顺序（剧本改编 / 资产拆解 / 资产图提示词 / 视频提示词）排好选中的环节 */
const ordered = (steps: readonly DramaStepKey[]): DramaStepKey[] =>
  PROMPT_STEPS.map((item) => item.key).filter((key) => steps.includes(key));

/**
 * 技能详情里的「短剧适用环节」多选：标了哪个环节，这个技能（已启用时）就会出现在用户
 * 开拍设置 / 提示词设置对应环节的「技能」下拉里。点「保存」才写入；内置技能只读。
 * @param item 技能
 * @param onSave 保存选中的环节；失败返回 null（全局提示已弹），成功返回更新后的技能
 */
export function DramaStepsField({
  item,
  onSave,
}: {
  item: SkillItem;
  onSave: (steps: DramaStepKey[]) => Promise<SkillItem | null>;
}) {
  const saved = ordered(item.drama_steps ?? []);
  const [draft, setDraft] = useState<DramaStepKey[]>(saved);
  const [saving, setSaving] = useState(false);
  const readonly = item.readonly;
  const dirty = draft.join() !== saved.join();

  const toggle = (key: DramaStepKey) =>
    setDraft((prev) =>
      ordered(prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]),
    );

  const save = async () => {
    setSaving(true);
    try {
      const next = await onSave(draft);
      if (next) setDraft(ordered(next.drama_steps ?? draft));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="grid gap-3" data-slot="drama-steps">
      <div className="flex flex-wrap gap-2" role="group" aria-label="短剧适用环节">
        {PROMPT_STEPS.map(({ key, name }) => {
          const on = draft.includes(key);
          return (
            <button
              key={key}
              type="button"
              aria-pressed={on}
              disabled={readonly || saving}
              title={readonly ? "内置技能不能标注短剧环节" : undefined}
              onClick={() => toggle(key)}
              className={cn(
                "focus-visible:ring-ring/50 inline-flex h-9 items-center gap-1.5 rounded-full border px-3.5 text-sm outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50",
                on ? "border-foreground bg-muted font-medium" : "hover:bg-muted/60",
              )}
            >
              {on && <Check className="size-3.5" />}
              {name}
            </button>
          );
        })}
      </div>
      {!readonly && (
        <div>
          <Button size="sm" disabled={!dirty || saving} onClick={() => void save()}>
            保存
          </Button>
        </div>
      )}
    </div>
  );
}
