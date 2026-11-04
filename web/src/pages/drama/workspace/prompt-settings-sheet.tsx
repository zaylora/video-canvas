import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import type { MyPrompt, PromptChoice, PromptSettings, PromptStep } from "@/api/drama/type";
import { PromptEditorPane } from "@/components/drama/prompts/prompt-editor";
import { PromptRows } from "@/components/drama/prompts/prompt-rows";
import {
  usePromptLibrary,
  type PromptLibrary,
} from "@/components/drama/prompts/use-prompt-library";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { PROMPT_STEPS, sameChoice } from "@/utils/drama/prompts";
import { promptsToChoices, type StartPrompts } from "@/utils/drama/start-settings";

import { useWorkspace } from "./context";

/** 已建剧的提示词设置转成可编辑的选择（只留来源 / id / 技能名） */
export function settingsToPrompts(settings: PromptSettings): StartPrompts {
  const out = {} as StartPrompts;
  for (const { key } of PROMPT_STEPS) {
    const { source, id, skill } = settings[key];
    out[key] = { source, ...(id ? { id } : {}), ...(skill ? { skill } : {}) };
  }
  return out;
}

/** 有哪些环节的选择和已保存的不一样 */
export function changedSteps(settings: PromptSettings, value: StartPrompts): PromptStep[] {
  return PROMPT_STEPS.filter(({ key }) => !sameChoice(settings[key], value[key])).map(
    ({ key }) => key,
  );
}

/**
 * 提示词设置的内容（Sheet 主体，每次打开重新挂载）：四行下拉，同开拍设置；新建 / 编辑「我的提示词」时整块换成编辑器。
 * 点「完成」才保存（PATCH prompts，后端重新快照正文），只影响之后发起的任务。
 */
export function PromptSettingsBody({
  library,
  onClose,
}: {
  /** 提示词库 */
  library: PromptLibrary;
  /** 关闭 Sheet */
  onClose: () => void;
}) {
  const { drama, patch } = useWorkspace();
  const [value, setValue] = useState<StartPrompts>(() => settingsToPrompts(drama.promptSettings));
  const [editor, setEditor] = useState<{ step: PromptStep; item: MyPrompt | null } | null>(null);
  const [saving, setSaving] = useState(false);
  const changed = changedSteps(drama.promptSettings, value);

  const done = async () => {
    if (changed.length === 0) {
      onClose();
      return;
    }
    setSaving(true);
    try {
      await patch({ prompts: promptsToChoices(value) });
      toast.success("已保存，之后的任务使用新提示词");
      onClose();
    } catch {
      /** 失败提示已由 patch 弹出，留在 Sheet 里方便改 */
      setSaving(false);
    }
  };

  if (editor) {
    return (
      <div className="overflow-y-auto p-4">
        <PromptEditorPane
          key={`${editor.step}:${editor.item?.id ?? "new"}`}
          step={editor.step}
          item={editor.item}
          library={library}
          onClose={(saved) => {
            if (saved) {
              setValue((prev) => ({ ...prev, [saved.step]: { source: "mine", id: saved.id } }));
            }
            setEditor(null);
          }}
        />
      </div>
    );
  }

  return (
    <>
      <SheetHeader>
        <SheetTitle>提示词设置</SheetTitle>
      </SheetHeader>
      <div className="mx-4 rounded-xl bg-chrome-hover/60 px-3 py-2 text-[13px]">
        修改只影响之后发起的任务
      </div>
      <div className="overflow-y-auto">
        <PromptRows
          kind={drama.sourceKind === "script" ? "script" : "novel"}
          value={value}
          library={library}
          saved={drama.promptSettings}
          onChange={(step: PromptStep, choice: PromptChoice) =>
            setValue((prev) => ({ ...prev, [step]: choice }))
          }
          onEdit={(step, item) => setEditor({ step, item })}
        />
      </div>
      <div className="mt-auto flex justify-end p-4">
        <Button className="rounded-full px-6" disabled={saving} onClick={() => void done()}>
          {saving && <Loader2 className="animate-spin" />}
          完成
        </Button>
      </div>
    </>
  );
}

/** 打开后才加载提示词库，关闭即释放 */
function PromptSettingsContent({ onClose }: { onClose: () => void }) {
  const library = usePromptLibrary();
  return <PromptSettingsBody library={library} onClose={onClose} />;
}

/**
 * 工作台「提示词设置」：顶栏项目配置胶囊和 ⌘K 命令面板都能打开。右侧滑出，内容同开拍设置里的四行提示词；
 * 保存后只影响之后发起的任务（已在跑的和已有的结果不变）。
 */
export function PromptSettingsSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="bg-chrome ring-chrome-border gap-2 backdrop-blur-xl backdrop-saturate-150 data-[side=right]:sm:max-w-md">
        {open && <PromptSettingsContent onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  );
}
