import { useMemo, useState } from "react";
import { ArrowLeft, Loader2 } from "lucide-react";

import { dramaErrorMessage, isRevisionConflict } from "@/api/drama/errors";
import type { MyPrompt, PromptStep, PromptTemplate } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import {
  PROMPT_BODY_MAX,
  PROMPT_NAME_MAX,
  draftFromMine,
  draftFromTemplate,
  promptStepName,
  restoreBuiltin,
  validatePromptDraft,
  type PromptDraft,
} from "@/utils/drama/prompts";

import type { PromptLibrary } from "./use-prompt-library";

/** 内置正文没加载到时的占位：页结构照旧，正文留空让用户自己写 */
const FALLBACK_PAGES: Record<PromptStep, PromptTemplate["pages"]> = {
  adapt: [
    { key: "plan", label: "分集规划", body: "" },
    { key: "scripts", label: "分集剧本", body: "" },
  ],
  assets: [{ key: "breakdown", label: "资产拆解", body: "" }],
  asset_prompts: [{ key: "asset_prompts", label: "资产图提示词", body: "" }],
  video: [{ key: "storyboard", label: "视频提示词", body: "" }],
};

/**
 * 提示词编辑器（开拍设置弹窗内侧滑 / 工作台 Sheet 共用）：名称、正文（剧本改编分「分集规划 / 分集剧本」两页）、
 * 字数、恢复内置、保存。只展示可替换的正文，不展示输入变量和输出结构（它们由系统在后台固定拼接）。
 * 保存成功后 onClose 带回保存的条目，调用方据此在当前行选中它；返回 / 取消传 null。
 */
export function PromptEditorPane({
  step,
  item,
  library,
  onClose,
}: {
  /** 环节 */
  step: PromptStep;
  /** 编辑的已有条目；新建为 null */
  item: MyPrompt | null;
  /** 提示词库 */
  library: PromptLibrary;
  /** 关闭编辑器：保存成功带条目，返回为 null */
  onClose: (saved: MyPrompt | null) => void;
}) {
  const template = useMemo<PromptTemplate>(
    () =>
      library.templates.find((entry) => entry.step === step) ?? {
        step,
        pages: FALLBACK_PAGES[step],
      },
    [library.templates, step],
  );
  const [draft, setDraft] = useState<PromptDraft>(() =>
    item ? draftFromMine(template, item) : draftFromTemplate(template),
  );
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const body = draft.bodies[draft.page] ?? "";

  const save = async () => {
    const problem = validatePromptDraft(draft, library.mine[step]);
    if (problem) {
      setError(problem);
      return;
    }
    setError(null);
    setSaving(true);
    try {
      onClose(await library.save(draft));
    } catch (e) {
      setError(
        isRevisionConflict(e)
          ? "这条提示词在别处被修改过，请返回后重新打开"
          : dramaErrorMessage(e, "保存失败，请稍后重试"),
      );
      setSaving(false);
    }
  };

  return (
    <div className="grid gap-4" data-slot="prompt-editor">
      <div className="flex items-center gap-1.5">
        <Button variant="ghost" size="icon-sm" aria-label="返回" onClick={() => onClose(null)}>
          <ArrowLeft />
        </Button>
        <div className="text-base font-semibold">
          {promptStepName(step)} · {item ? "编辑" : "新建"}
        </div>
      </div>
      <Input
        value={draft.name}
        maxLength={PROMPT_NAME_MAX}
        placeholder="名称"
        aria-label="名称"
        onChange={(event) => setDraft({ ...draft, name: event.target.value })}
        className="bg-chrome-hover border-chrome-border h-10 rounded-full px-4"
      />
      {draft.pages.length > 1 && (
        <div
          role="tablist"
          aria-label="提示词页"
          className="bg-chrome-hover ring-chrome-border flex w-fit gap-0.5 rounded-full p-0.5 ring-1"
        >
          {draft.pages.map((page) => (
            <button
              key={page.key}
              type="button"
              role="tab"
              aria-selected={draft.page === page.key}
              onClick={() => setDraft({ ...draft, page: page.key })}
              className={cn(
                "focus-visible:ring-ring/50 h-8 rounded-full px-4 text-[13px] outline-none focus-visible:ring-2",
                draft.page === page.key
                  ? "bg-chrome ring-chrome-border font-medium shadow-sm ring-1"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {page.label}
            </button>
          ))}
        </div>
      )}
      <div className="grid gap-1.5">
        <Textarea
          value={body}
          aria-label="提示词正文"
          onChange={(event) =>
            setDraft({ ...draft, bodies: { ...draft.bodies, [draft.page]: event.target.value } })
          }
          className="bg-chrome-hover border-chrome-border h-72 resize-none rounded-2xl px-4 py-3 font-mono text-[13px] leading-relaxed"
        />
        <div
          className={cn(
            "text-muted-foreground text-right font-mono text-[11.5px] tabular-nums",
            body.length > PROMPT_BODY_MAX && "text-destructive",
          )}
        >
          {body.length.toLocaleString("en-US")} / {PROMPT_BODY_MAX.toLocaleString("en-US")}
        </div>
      </div>
      {error && (
        <p role="alert" className="text-destructive text-[13px]">
          {error}
        </p>
      )}
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          className="mr-auto rounded-full"
          disabled={saving}
          onClick={() => {
            setDraft(restoreBuiltin(draft, template, draft.page));
            setError(null);
          }}
        >
          恢复内置
        </Button>
        <Button className="rounded-full px-5" disabled={saving} onClick={() => void save()}>
          {saving && <Loader2 className="animate-spin" />}
          保存
        </Button>
      </div>
    </div>
  );
}
