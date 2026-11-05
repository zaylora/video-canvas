import { useCallback, useEffect, useRef, useState } from "react";

import {
  createMyPrompt,
  deleteMyPrompt,
  listDramaSkills,
  listMyPrompts,
  listPromptTemplates,
  updateMyPrompt,
} from "@/api/drama";
import type { DramaSkill, MyPrompt, PromptStep, PromptTemplate } from "@/api/drama/type";
import { PROMPT_STEPS } from "@/utils/drama/prompts";
import type { PromptDraft } from "@/utils/drama/prompts";

/** 提示词库：内置正文、我的提示词、可用技能，以及对「我的提示词」的增改删 */
export interface PromptLibrary {
  /** 是否已加载完（成功或失败） */
  ready: boolean;
  /** 各环节的内置正文 */
  templates: PromptTemplate[];
  /** 各环节的我的提示词，按更新时间倒序 */
  mine: Record<PromptStep, MyPrompt[]>;
  /** 各环节可用的技能 */
  skills: Record<PromptStep, DramaSkill[]>;
  /**
   * 保存草稿：新建或修改；67017 / 409 等错误向上抛，由编辑器就地展示
   * @param draft 编辑器里的草稿
   * @returns 保存后的条目
   */
  save: (draft: PromptDraft) => Promise<MyPrompt>;
  /**
   * 删除一条我的提示词；已建的短剧不受影响（快照）
   * @param item 要删的条目
   */
  remove: (item: MyPrompt) => Promise<void>;
}

const emptyByStep = <T>(): Record<PromptStep, T[]> => ({
  adapt: [],
  assets: [],
  asset_prompts: [],
  video: [],
});

/**
 * 提示词库：挂载时一次拉齐内置正文、我的提示词和各环节的可用技能；
 * 开拍设置弹窗与工作台「提示词设置」共用。读取失败的那一项按空处理（全局 toast 已提示）。
 */
export function usePromptLibrary(): PromptLibrary {
  const [ready, setReady] = useState(false);
  const [templates, setTemplates] = useState<PromptTemplate[]>([]);
  const [mine, setMine] = useState<Record<PromptStep, MyPrompt[]>>(emptyByStep);
  const [skills, setSkills] = useState<Record<PromptStep, DramaSkill[]>>(emptyByStep);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    void (async () => {
      const [tpl, mineAll, ...skillLists] = await Promise.allSettled([
        listPromptTemplates(),
        listMyPrompts(),
        ...PROMPT_STEPS.map((item) => listDramaSkills(item.key)),
      ]);
      if (!alive.current) return;
      if (tpl.status === "fulfilled") setTemplates(tpl.value);
      if (mineAll.status === "fulfilled") {
        const grouped = emptyByStep<MyPrompt>();
        for (const item of mineAll.value) grouped[item.step]?.push(item);
        setMine(grouped);
      }
      const bySkill = emptyByStep<DramaSkill>();
      skillLists.forEach((result, index) => {
        if (result.status === "fulfilled") bySkill[PROMPT_STEPS[index].key] = result.value;
      });
      setSkills(bySkill);
      setReady(true);
    })();
    return () => {
      alive.current = false;
    };
  }, []);

  const save = useCallback(async (draft: PromptDraft): Promise<MyPrompt> => {
    const input = { step: draft.step, name: draft.name.trim(), bodies: draft.bodies };
    const saved = draft.id
      ? await updateMyPrompt(draft.id, { ...input, revision: draft.revision })
      : await createMyPrompt(input);
    if (alive.current) {
      setMine((prev) => ({
        ...prev,
        [saved.step]: [saved, ...prev[saved.step].filter((item) => item.id !== saved.id)],
      }));
    }
    return saved;
  }, []);

  const remove = useCallback(async (item: MyPrompt) => {
    await deleteMyPrompt(item.id);
    if (alive.current) {
      setMine((prev) => ({
        ...prev,
        [item.step]: prev[item.step].filter((entry) => entry.id !== item.id),
      }));
    }
  }, []);

  return { ready, templates, mine, skills, save, remove };
}
